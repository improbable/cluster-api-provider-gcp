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
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/scope"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/services/managedcompute"
	infrav1exp "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GCPManagedControlPlaneReconciler reconciles a GCPManagedControlPlane object
type GCPManagedControlPlaneReconciler struct {
	client.Client
	Log logr.Logger
}

func (r *GCPManagedControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&infrav1exp.GCPManagedControlPlane{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=exp.infrastructure.cluster.x-k8s.io,resources=gcpmanagedcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=exp.infrastructure.cluster.x-k8s.io,resources=gcpmanagedcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

func (r *GCPManagedControlPlaneReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.TODO()
	log := r.Log.WithValues("namespace", req.Namespace, "gcpManagedControlPlane", req.Name)

	// Fetch the GCPManagedControlPlane instance
	gcpControlPlane := &infrav1exp.GCPManagedControlPlane{}
	err := r.Get(ctx, req.NamespacedName, gcpControlPlane)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, gcpControlPlane.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	// fetch default pool
	defaultPoolKey := client.ObjectKey{
		Name:      gcpControlPlane.Spec.DefaultPoolRef.Name,
		Namespace: gcpControlPlane.Namespace,
	}
	defaultPool := &infrav1exp.GCPManagedMachinePool{}
	if err := r.Client.Get(ctx, defaultPoolKey, defaultPool); err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to fetch default pool reference")
	}

	log = log.WithValues("gcpManagedMachinePool", defaultPoolKey.Name)

	// fetch owner of default pool
	// TODO(ace): create a helper in util for this
	// Fetch the owning MachinePool.
	ownerPool, err := getOwnerMachinePool(ctx, r.Client, defaultPool.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if ownerPool == nil {
		log.Info("failed to fetch owner ref for default pool")
		return reconcile.Result{}, nil
	}

	log = log.WithValues("machinePool", ownerPool.Name)

	// Create the scope.
	mcpScope, err := scope.NewManagedControlPlaneScope(scope.ManagedControlPlaneScopeParams{
		Client:           r.Client,
		Logger:           log,
		Cluster:          cluster,
		ControlPlane:     gcpControlPlane,
		MachinePool:      ownerPool,
		InfraMachinePool: defaultPool,
		PatchTarget:      gcpControlPlane,
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

	// Handle deleted clusters
	if !gcpControlPlane.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, mcpScope)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, mcpScope)
}

func (r *GCPManagedControlPlaneReconciler) reconcileDelete(ctx context.Context, scope *scope.ManagedControlPlaneScope) (ctrl.Result, error) {
	scope.Info("Handling deleted GCPManagedControlPlane")

	// TODO: Delete GKE cluster
	computeSvc := managedcompute.NewService(scope)

	if err := computeSvc.DeleteGKECluster(); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to delete GKE cluster for GCPManagedControlPlane %s/%s", scope.ControlPlane.Namespace, scope.ControlPlane.Name)
	}

	if err := computeSvc.DeleteNetwork(); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to delete network for GCPManagedControlPlane %s/%s", scope.ControlPlane.Namespace, scope.ControlPlane.Name)
	}

	// Cluster is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(scope.ControlPlane, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}

func (r *GCPManagedControlPlaneReconciler) reconcileNormal(ctx context.Context, scope *scope.ManagedControlPlaneScope) (ctrl.Result, error) {
	scope.Info("Reconciling GCPManagedControlPlane")

	// If the GCPManagedControlPlane doesn't have our finalizer, add it.
	controllerutil.AddFinalizer(scope.ControlPlane, infrav1exp.ManagedControlPlaneFinalizer)
	// Register the finalizer immediately to avoid orphaning GCP resources on delete
	if err := scope.PatchObject(ctx); err != nil {
		return reconcile.Result{}, err
	}

	computeSvc := managedcompute.NewService(scope)

	if err := computeSvc.ReconcileNetwork(); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to reconcile network for GCPManagedControlPlane %s/%s", scope.ControlPlane.Namespace, scope.ControlPlane.Name)
	}

	if err := computeSvc.ReconcileGKECluster(); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to reconcile GKE cluster for GCPManagedControlPlane %s/%s", scope.ControlPlane.Namespace, scope.ControlPlane.Name)
	}

	// No errors, so mark us ready so the Cluster API Cluster Controller can pull it
	scope.ControlPlane.Status.Ready = true
	scope.ControlPlane.Status.Initialized = true

	return ctrl.Result{}, nil
}
