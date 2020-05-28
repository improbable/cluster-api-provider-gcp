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
	"k8s.io/apimachinery/pkg/types"
	infrav1exp "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// GCPManagedClusterReconciler reconciles a GCPManagedCluster object
type GCPManagedClusterReconciler struct {
	client.Client
	Log logr.Logger
}

func (r *GCPManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&infrav1exp.GCPManagedCluster{}).
		// Watch for changes to the control plane so we can reconcile
		Watches(&source.Kind{Type: &infrav1exp.GCPManagedControlPlane{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.requeueGCPClusterForControlPlane),
			}).
		Complete(r)
}

// +kubebuilder:rbac:groups=exp.infrastructure.cluster.x-k8s.io,resources=gcpmanagedclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=exp.infrastructure.cluster.x-k8s.io,resources=gcpmanagedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *GCPManagedClusterReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.TODO()
	log := r.Log.WithValues("namespace", req.Namespace, "gcpManagedCluster", req.Name)
	log.Info("Reconciling GCPManagedCluster")

	// Fetch the GCPManagedCluster instance
	gcpCluster := &infrav1exp.GCPManagedCluster{}
	err := r.Get(ctx, req.NamespacedName, gcpCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, gcpCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	// Fetch the GCPManagedControlPlane for the cluster
	controlPlane := &infrav1exp.GCPManagedControlPlane{}
	controlPlaneRef := types.NamespacedName{
		Name:      cluster.Spec.ControlPlaneRef.Name,
		Namespace: cluster.Namespace,
	}

	if err := r.Get(ctx, controlPlaneRef, controlPlane); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get control plane ref %v", controlPlaneRef)
	}

	log = log.WithValues("controlPlane", controlPlaneRef.Name)

	patchHelper, err := patch.NewHelper(gcpCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to init patch helper")
	}

	// Match whatever the control plane says. We should also enqueue
	// requests from control plane to infra cluster to keep this accurate
	gcpCluster.Status.Ready = controlPlane.Status.Ready
	gcpCluster.Spec.ControlPlaneEndpoint = controlPlane.Spec.ControlPlaneEndpoint

	if err := patchHelper.Patch(ctx, gcpCluster); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled")

	return ctrl.Result{}, nil
}

func (r *GCPManagedClusterReconciler) requeueGCPClusterForControlPlane(o handler.MapObject) []ctrl.Request {
	cp, ok := o.Object.(*infrav1exp.GCPManagedControlPlane)
	if !ok {
		r.Log.Error(errors.Errorf("expected a GCPManagedControlPlane but got a %T", o.Object),
			"failed to get GCPManagedCluster for GCPManagedControlPlane")
		return nil
	}

	c, err := util.GetOwnerCluster(context.TODO(), r.Client, cp.ObjectMeta)
	switch {
	case err != nil:
		r.Log.Error(err, "failed to get owning cluster")
		return nil
	case apierrors.IsNotFound(err) || c == nil || c.Spec.ControlPlaneRef == nil:
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{Namespace: c.Namespace, Name: c.Spec.ControlPlaneRef.Name},
		},
	}
}
