/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/scope"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/services/managedcompute"
	infrav1exp "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	capiv1exp "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GCPManagedMachinePoolReconciler reconciles a GCPManagedMachinePool object
type GCPManagedMachinePoolReconciler struct {
	client.Client
	Log logr.Logger
}

func (r *GCPManagedMachinePoolReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&infrav1exp.GCPManagedMachinePool{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=exp.infrastructure.cluster.x-k8s.io,resources=gcpmanagedmachinepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=exp.infrastructure.cluster.x-k8s.io,resources=gcpmanagedmachinepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch

func (r *GCPManagedMachinePoolReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.TODO()
	log := r.Log.WithValues("namespace", req.Namespace, "infraPool", req.Name)

	// Fetch the GCPManagedMachinePool instance
	infraPool := &infrav1exp.GCPManagedMachinePool{}
	err := r.Get(ctx, req.NamespacedName, infraPool)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the owning MachinePool.
	ownerPool, err := getOwnerMachinePool(ctx, r.Client, infraPool.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if ownerPool == nil {
		log.Info("MachinePool Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	// Fetch the Cluster.
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, ownerPool.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if ownerCluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	log = log.WithValues("ownerCluster", ownerCluster.Name)

	// Fetch the corresponding control plane which has all the interesting data.
	controlPlane := &infrav1exp.GCPManagedControlPlane{}
	controlPlaneName := client.ObjectKey{
		Namespace: ownerCluster.Spec.ControlPlaneRef.Namespace,
		Name:      ownerCluster.Spec.ControlPlaneRef.Name,
	}
	if err := r.Client.Get(ctx, controlPlaneName, controlPlane); err != nil {
		return reconcile.Result{}, err
	}

	// Create the scope.
	mcpScope, err := scope.NewManagedControlPlaneScope(scope.ManagedControlPlaneScopeParams{
		Client:           r.Client,
		Logger:           log,
		ControlPlane:     controlPlane,
		Cluster:          ownerCluster,
		MachinePool:      ownerPool,
		InfraMachinePool: infraPool,
		PatchTarget:      infraPool,
	})
	if err != nil {
		return reconcile.Result{}, errors.Errorf("failed to create scope: %+v", err)
	}
	// Always patch when exiting so we can persist changes to finalizers and status
	defer func() {
		if err := mcpScope.PatchObject(ctx); err != nil && reterr == nil {
			reterr = err
		}
	}()

	// Handle deleted machine pool
	if !infraPool.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, mcpScope)
	}

	// Handle non-deleted machine pool
	return r.reconcileNormal(ctx, mcpScope)

}

func (r *GCPManagedMachinePoolReconciler) reconcileDelete(ctx context.Context, scope *scope.ManagedControlPlaneScope) (ctrl.Result, error) {
	// TODO: Implement delete
	return ctrl.Result{}, nil
}

func (r *GCPManagedMachinePoolReconciler) reconcileNormal(ctx context.Context, scope *scope.ManagedControlPlaneScope) (ctrl.Result, error) {
	scope.Logger.Info("Reconciling GCPManagedMachinePool")

	// If the GCPManagedMachinePool doesn't have our finalizer, add it.
	controllerutil.AddFinalizer(scope.InfraMachinePool, infrav1.ClusterFinalizer)
	// Register the finalizer immediately to avoid orphaning GCP resources on delete
	if err := scope.PatchObject(ctx); err != nil {
		return reconcile.Result{}, err
	}

	if !scope.ControlPlane.Status.Ready {
		return reconcile.Result{}, errors.Errorf("GCPManagedControlPlane is not ready yet")
	}

	computeSvc := managedcompute.NewService(scope)

	if err := computeSvc.ReconcileGKENodePool(); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to reconcile GKE node pool for GCPManagedMachinePool %s/%s", scope.InfraMachinePool.Namespace, scope.InfraMachinePool.Name)
	}

	// No errors, so mark us ready so the Cluster API Cluster Controller can pull it
	scope.InfraMachinePool.Status.Ready = true

	return reconcile.Result{}, nil
}

// getOwnerMachinePool returns the MachinePool object owning the current resource.
func getOwnerMachinePool(ctx context.Context, c client.Client, obj metav1.ObjectMeta) (*capiv1exp.MachinePool, error) {
	for _, ref := range obj.OwnerReferences {
		if ref.Kind == "MachinePool" && ref.APIVersion == capiv1exp.GroupVersion.String() {
			return getMachinePoolByName(ctx, c, obj.Namespace, ref.Name)
		}
	}
	return nil, nil
}

// getMachinePoolByName finds and return a Machine object using the specified params.
func getMachinePoolByName(ctx context.Context, c client.Client, namespace, name string) (*capiv1exp.MachinePool, error) {
	m := &capiv1exp.MachinePool{}
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := c.Get(ctx, key, m); err != nil {
		return nil, err
	}
	return m, nil
}
