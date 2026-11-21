/*
Copyright 2023 The Kubernetes Authors.

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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/api/v1alpha2"
	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/internal/poolstats"
	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/internal/poolutil"
	pooltypes "sigs.k8s.io/cluster-api-ipam-provider-in-cluster/pkg/types"
)

const (
	inClusterIPPoolKind       = "InClusterIPPool"
	globalInClusterIPPoolKind = "GlobalInClusterIPPool"

	// ProtectPoolFinalizer is used to prevent deletion of a Pool object while its addresses have not been deleted.
	ProtectPoolFinalizer = "ipam.cluster.x-k8s.io/ProtectPool"
)

// InClusterIPPoolReconciler reconciles a InClusterIPPool object.
type InClusterIPPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *InClusterIPPoolReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha2.InClusterIPPool{}).
		Watches(
			&ipamv1.IPAddress{},
			handler.EnqueueRequestsFromMapFunc(r.ipAddressToInClusterIPPool)).
		Complete(r)
}

func (r *InClusterIPPoolReconciler) ipAddressToInClusterIPPool(_ context.Context, clientObj client.Object) []reconcile.Request {
	ipAddress, ok := clientObj.(*ipamv1.IPAddress)
	if !ok {
		return nil
	}

	if ipAddress.Spec.PoolRef.APIGroup == v1alpha2.GroupVersion.Group &&
		ipAddress.Spec.PoolRef.Kind == inClusterIPPoolKind {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: ipAddress.Namespace,
				Name:      ipAddress.Spec.PoolRef.Name,
			},
		}}
	}

	return nil
}

// GlobalInClusterIPPoolReconciler reconciles a GlobalInClusterIPPool object.
type GlobalInClusterIPPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *GlobalInClusterIPPoolReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha2.GlobalInClusterIPPool{}).
		Watches(
			&ipamv1.IPAddress{},
			handler.EnqueueRequestsFromMapFunc(r.ipAddressToGlobalInClusterIPPool)).
		Complete(r)
}

func (r *GlobalInClusterIPPoolReconciler) ipAddressToGlobalInClusterIPPool(_ context.Context, clientObj client.Object) []reconcile.Request {
	ipAddress, ok := clientObj.(*ipamv1.IPAddress)
	if !ok {
		return nil
	}

	if ipAddress.Spec.PoolRef.APIGroup == v1alpha2.GroupVersion.Group &&
		ipAddress.Spec.PoolRef.Kind == globalInClusterIPPoolKind {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: ipAddress.Namespace,
				Name:      ipAddress.Spec.PoolRef.Name,
			},
		}}
	}

	return nil
}

//+kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=inclusterippools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=inclusterippools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=inclusterippools/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *InClusterIPPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling pool")

	pool := &v1alpha2.InClusterIPPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "failed to fetch InClusterIPPool")
		}
		return ctrl.Result{}, nil
	}
	return genericReconcile(ctx, r.Client, pool)
}

//+kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=globalinclusterippools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=globalinclusterippools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ipam.cluster.x-k8s.io,resources=globalinclusterippools/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *GlobalInClusterIPPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling pool")

	pool := &v1alpha2.GlobalInClusterIPPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "failed to fetch GlobalInClusterIPPool")
		}
		return ctrl.Result{}, nil
	}
	return genericReconcile(ctx, r.Client, pool)
}

func genericReconcile(ctx context.Context, c client.Client, pool pooltypes.GenericInClusterPool) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	poolTypeRef := ipamv1.IPPoolReference{
		APIGroup: v1alpha2.GroupVersion.Group,
		Kind:     pool.GetObjectKind().GroupVersionKind().Kind,
		Name:     pool.GetName(),
	}

	// The in-use addresses and the pool spec are the single snapshot from which all
	// statistics are derived. The status is never consulted for allocation decisions.
	addressesInUse, err := poolutil.ListAddressesInUse(ctx, c, pool.GetNamespace(), poolTypeRef)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to list addresses")
	}

	inUseCount := len(addressesInUse)
	hasFinalizer := controllerutil.ContainsFinalizer(pool, ProtectPoolFinalizer)

	// While the pool is deleting it must not receive status updates: the finalizer
	// keeps the object around until every allocation is released, and once the object
	// is gone the top-level Reconcile handles NotFound without ever reaching here.
	if !pool.GetDeletionTimestamp().IsZero() {
		if inUseCount != 0 || !hasFinalizer {
			return ctrl.Result{}, nil
		}
		patchHelper, err := patch.NewHelper(pool, c)
		if err != nil {
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(pool, ProtectPoolFinalizer)
		return ctrl.Result{}, patchHelper.Patch(ctx, pool)
	}

	stats, err := poolstats.ComputeIPPoolStats(pool.PoolSpec(), addressesInUse)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to compute pool statistics")
	}

	// Decide whether anything must be written before mutating the object, so a
	// steady-state reconcile neither builds a patch helper nor emits an API call.
	needsFinalizer := !hasFinalizer
	statusChanged := !stats.EqualsIPStatus(pool.PoolStatus().Addresses)
	if !needsFinalizer && !statusChanged {
		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(pool, c)
	if err != nil {
		return ctrl.Result{}, err
	}

	if needsFinalizer {
		controllerutil.AddFinalizer(pool, ProtectPoolFinalizer)
	}
	if statusChanged {
		pool.PoolStatus().Addresses = stats.ToIPStatus()
	}

	log.Info("Updating pool with usage info", "statusAddresses", pool.PoolStatus().Addresses)
	return ctrl.Result{}, patchHelper.Patch(ctx, pool)
}
