/*
Copyright 2026 The Kubernetes Authors.

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
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/api/v1alpha2"
	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/internal/index"
	webhooks "sigs.k8s.io/cluster-api-ipam-provider-in-cluster/internal/webhooks"
)

var errInjected = errors.New("injected list failure")

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	gomega.NewWithT(t).Expect(v1alpha2.AddToScheme(scheme)).To(gomega.Succeed())
	gomega.NewWithT(t).Expect(ipamv1.AddToScheme(scheme)).To(gomega.Succeed())
	return scheme
}

func memoryPool() *v1alpha2.InClusterIPPool {
	return &v1alpha2.InClusterIPPool{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha2.GroupVersion.String(),
			Kind:       "InClusterIPPool",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "memory-pool", Namespace: "default"},
		Spec: v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"10.0.0.10-10.0.0.12"},
			Prefix:                      24,
			AllocateReservedIPAddresses: true,
		},
	}
}

// restoreGVK re-applies the TypeMeta that a real apiserver populates but the fake
// client strips on Get/List, so the controller can build a correct PoolRef.
func restoreGVK(pool *v1alpha2.InClusterIPPool) {
	pool.TypeMeta = metav1.TypeMeta{
		APIVersion: v1alpha2.GroupVersion.String(),
		Kind:       "InClusterIPPool",
	}
}

func memoryAddress(name string, pool *v1alpha2.InClusterIPPool) *ipamv1.IPAddress {
	return &ipamv1.IPAddress{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pool.Namespace},
		Spec: ipamv1.IPAddressSpec{
			Address: name, // tests set meaningful addresses where required
			PoolRef: ipamv1.IPPoolReference{
				APIGroup: v1alpha2.GroupVersion.Group,
				Kind:     "InClusterIPPool",
				Name:     pool.Name,
			},
		},
	}
}

type writeCounter struct {
	mu                sync.Mutex
	patches           atomic.Int64
	statusPatches     atomic.Int64
	statusUpdates     atomic.Int64
	failListWithError error
}

func (w *writeCounter) totalWrites() int64 {
	return w.patches.Load() + w.statusPatches.Load() + w.statusUpdates.Load()
}

// newCountingClient builds a fake client configured with a status subresource so
// regular PATCH calls cannot touch status (giving the write assertions real meaning),
// wrapped in an interceptor that records every mutating call.
func newCountingClient(t *testing.T, counter *writeCounter, objs ...client.Object) client.Client {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objs...).
		WithIndex(&ipamv1.IPAddress{}, index.IPAddressPoolRefCombinedField, index.IPAddressByCombinedPoolRef).
		WithStatusSubresource(
			&v1alpha2.InClusterIPPool{},
			&v1alpha2.GlobalInClusterIPPool{},
			&v1alpha2.InClusterPrefixPool{},
			&v1alpha2.GlobalInClusterPrefixPool{},
		)

	return interceptor.NewClient(builder.Build(), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			counter.mu.Lock()
			listErr := counter.failListWithError
			counter.mu.Unlock()
			if listErr != nil {
				return listErr
			}
			return c.List(ctx, list, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			counter.patches.Add(1)
			return c.Patch(ctx, obj, patch, opts...)
		},
		SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			counter.statusPatches.Add(1)
			return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			counter.statusUpdates.Add(1)
			return c.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	})
}

func TestGenericReconcileIdempotentStatusWrites(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pool := memoryPool()
	counter := &writeCounter{}
	c := newCountingClient(t, counter, pool)

	// First reconcile: the finalizer goes out as a metadata patch and the status as
	// a separate status patch (two API calls for one logical reconcile).
	_, err := genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counter.patches.Load()).To(gomega.Equal(int64(1)), "one metadata patch for the finalizer")
	g.Expect(counter.statusPatches.Load()).To(gomega.Equal(int64(1)), "one status patch for the initial stats")

	fetched := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), fetched)).To(gomega.Succeed())
	restoreGVK(fetched)
	resourceVersionAfterFirst := fetched.ResourceVersion
	g.Expect(fetched.Finalizers).To(gomega.ContainElement(ProtectPoolFinalizer))
	g.Expect(fetched.Status.Addresses).ToNot(gomega.BeNil())
	g.Expect(fetched.Status.Addresses.Total).To(gomega.Equal(3))
	g.Expect(fetched.Status.Addresses.Free).To(gomega.Equal(3))

	// Second reconcile against an unchanged snapshot must not write anything at all.
	_, err = genericReconcile(ctx, c, fetched)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counter.totalWrites()).To(gomega.Equal(int64(2)), "identical status must not produce a write")

	again := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), again)).To(gomega.Succeed())
	g.Expect(again.ResourceVersion).To(gomega.Equal(resourceVersionAfterFirst), "resourceVersion must not change without a write")
}

func TestGenericReconcileWritesOnlyWhenNumbersChange(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pool := memoryPool()
	counter := &writeCounter{}
	c := newCountingClient(t, counter, pool)

	_, err := genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	statusWritesAfterInitial := counter.statusPatches.Load()

	// Add an allocation: the status content changes so another status write is
	// expected, but only one even though a burst of reconcile calls arrives for the
	// single event.
	addr := memoryAddress("10.0.0.10", pool)
	addr.Spec.Address = "10.0.0.10"
	g.Expect(c.Create(ctx, addr)).To(gomega.Succeed())

	for range 10 {
		current := &v1alpha2.InClusterIPPool{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), current)).To(gomega.Succeed())
		restoreGVK(current)
		_, err = genericReconcile(ctx, c, current)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	g.Expect(counter.statusPatches.Load()).To(gomega.Equal(statusWritesAfterInitial+1), "the burst coalesces into one status write")
	g.Expect(counter.patches.Load()).To(gomega.BeNumerically("<=", int64(1)), "no metadata churn beyond the initial finalizer handling")

	final := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), final)).To(gomega.Succeed())
	g.Expect(final.Status.Addresses.Used).To(gomega.Equal(1))
	g.Expect(final.Status.Addresses.Free).To(gomega.Equal(2))
	g.Expect(final.Status.Addresses.Exhausted).To(gomega.BeFalse())
}

func TestGenericReconcileDoesNotWriteStatusWhenListFails(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pool := memoryPool()
	counter := &writeCounter{failListWithError: errInjected}
	c := newCountingClient(t, counter, pool)

	_, err := genericReconcile(ctx, c, pool)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(counter.totalWrites()).To(gomega.Equal(int64(0)), "a failed list must not leave any status write behind")
}

func TestGenericReconcilePoolsAreIndependent(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	poolA := memoryPool()
	poolB := memoryPool()
	poolB.Name = "other-pool"

	counter := &writeCounter{}
	c := newCountingClient(t, counter, poolA, poolB)

	// Reconcile both pools to their initial steady state.
	_, err := genericReconcile(ctx, c, poolA)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	_, err = genericReconcile(ctx, c, poolB)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	steadyStatusWrites := counter.statusPatches.Load()

	// An address event belonging to pool A must not cause a write when pool B is
	// reconciled, because the address list for B is unchanged.
	addrA := memoryAddress("10.0.0.10", poolA)
	addrA.Spec.Address = "10.0.0.10"
	g.Expect(c.Create(ctx, addrA)).To(gomega.Succeed())

	currentB := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(poolB), currentB)).To(gomega.Succeed())
	restoreGVK(currentB)
	_, err = genericReconcile(ctx, c, currentB)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counter.statusPatches.Load()).To(gomega.Equal(steadyStatusWrites), "reconciling pool B must not write on a pool A event")

	currentA := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(poolA), currentA)).To(gomega.Succeed())
	restoreGVK(currentA)
	_, err = genericReconcile(ctx, c, currentA)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// Pool A gained one further status write; pool B's counter is unchanged in
	// absolute terms because every write is keyed to its owning pool object.
	g.Expect(counter.statusPatches.Load()).To(gomega.Equal(steadyStatusWrites+1), "only the owning pool updates its status")
}

func TestGenericReconcileDeletionBlocksAndSkipsStatus(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pool := memoryPool()
	addr := memoryAddress("10.0.0.10", pool)
	addr.Spec.Address = "10.0.0.10"

	counter := &writeCounter{}
	c := newCountingClient(t, counter, pool, addr)

	_, err := genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Mark the pool for deletion while an allocation still exists: the finalizer
	// must remain and no status update may be produced.
	statusWritesBeforeDelete := counter.statusPatches.Load()
	patchWritesBeforeDelete := counter.patches.Load()
	now := metav1.Now()
	pool.DeletionTimestamp = &now
	_, err = genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(counter.statusPatches.Load()).To(gomega.Equal(statusWritesBeforeDelete), "deleting pool with allocations must not touch status")
	g.Expect(counter.patches.Load()).To(gomega.Equal(patchWritesBeforeDelete), "deleting pool with allocations must not touch finalizers")

	stored := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), stored)).To(gomega.Succeed())
	g.Expect(stored.Finalizers).To(gomega.ContainElement(ProtectPoolFinalizer), "the finalizer blocks deletion while addresses exist")

	// Once the allocation is gone, the finalizer is removed (a metadata-only write).
	g.Expect(c.DeleteAllOf(ctx, &ipamv1.IPAddress{}, client.InNamespace(pool.Namespace))).To(gomega.Succeed())
	_, err = genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	final := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), final)).To(gomega.Succeed())
	g.Expect(final.Finalizers).NotTo(gomega.ContainElement(ProtectPoolFinalizer))
}

func TestGenericReconcileForcedDeletionProducesNoStatusErrors(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pool := memoryPool()
	pool.Annotations = map[string]string{webhooks.SkipValidateDeleteWebhookAnnotation: ""}
	addr := memoryAddress("10.0.0.10", pool)
	addr.Spec.Address = "10.0.0.10"

	counter := &writeCounter{}
	c := newCountingClient(t, counter, pool, addr)

	_, err := genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Simulate forced deletion actually removing the object: the top-level reconcile
	// treats NotFound as a quiet no-op and never attempts a status update.
	g.Expect(c.Delete(ctx, pool)).To(gomega.Succeed())
	reconciler := &InClusterIPPoolReconciler{Client: c, Scheme: c.Scheme()}
	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)})
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

// TestGenericReconcileConcurrentEventsCoalesce models a high-frequency allocation
// burst followed by a storm of redundant reconcile events. It asserts the final
// numbers are correct and the number of status writes is far smaller than the number
// of reconcile events.
func TestGenericReconcileConcurrentEventsCoalesce(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	pool := memoryPool()
	counter := &writeCounter{}
	c := newCountingClient(t, counter, pool)

	// Initial reconcile establishes finalizer + baseline status.
	_, err := genericReconcile(ctx, c, pool)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	baselineStatusWrites := counter.statusPatches.Load()

	// Fill the 3-address pool: each allocation changes used/free and causes one write.
	for i, addrStr := range []string{"10.0.0.10", "10.0.0.11", "10.0.0.12"} {
		addr := memoryAddress("alloc-"+string(rune('0'+i)), pool)
		addr.Spec.Address = addrStr
		g.Expect(c.Create(ctx, addr)).To(gomega.Succeed())

		current := &v1alpha2.InClusterIPPool{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), current)).To(gomega.Succeed())
		restoreGVK(current)
		_, err = genericReconcile(ctx, c, current)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	filled := &v1alpha2.InClusterIPPool{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), filled)).To(gomega.Succeed())
	restoreGVK(filled)
	g.Expect(filled.Status.Addresses.Used).To(gomega.Equal(3))
	g.Expect(filled.Status.Addresses.Free).To(gomega.Equal(0))
	g.Expect(filled.Status.Addresses.Exhausted).To(gomega.BeTrue())

	writesAfterFill := counter.statusPatches.Load()

	// A burst of 50 reconcile events against the unchanged, already-current state
	// must produce zero additional writes.
	const redundantEvents = 50
	for range redundantEvents {
		current := &v1alpha2.InClusterIPPool{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pool), current)).To(gomega.Succeed())
		restoreGVK(current)
		_, err = genericReconcile(ctx, c, current)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}

	totalEvents := 3 + redundantEvents
	statusWrites := counter.statusPatches.Load() - baselineStatusWrites
	g.Expect(counter.statusPatches.Load()).To(gomega.Equal(writesAfterFill),
		"the redundant event storm must not create a single status write")
	g.Expect(statusWrites).To(gomega.BeNumerically("<", int64(totalEvents)),
		"status writes coalesce: %d writes for %d reconcile events", statusWrites, totalEvents)
}
