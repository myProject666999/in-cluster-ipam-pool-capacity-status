/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controllers

import (
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/api/v1alpha2"
)

func ipUpdateEvent(old, new client.Object) event.TypedUpdateEvent[client.Object] {
	return event.TypedUpdateEvent[client.Object]{ObjectOld: old, ObjectNew: new}
}

func TestPoolNoLongerEmptyPredicate(t *testing.T) {
	g := gomega.NewWithT(t)
	predicate := poolNoLongerEmpty()

	poolWith := func(free int, exhausted bool) *v1alpha2.InClusterIPPool {
		return &v1alpha2.InClusterIPPool{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Status: v1alpha2.InClusterIPPoolStatus{
				Addresses: &v1alpha2.InClusterIPPoolStatusIPAddresses{
					Free:      free,
					Exhausted: exhausted,
				},
			},
		}
	}

	// The predicate fires only on the Free 0 -> positive transition; the new bool
	// fields must not change that contract.
	g.Expect(predicate.Update(ipUpdateEvent(poolWith(0, true), poolWith(0, true)))).
		To(gomega.BeFalse(), "staying exhausted must not re-enqueue claims")
	g.Expect(predicate.Update(ipUpdateEvent(poolWith(1, false), poolWith(2, false)))).
		To(gomega.BeFalse(), "positive-to-positive free changes do not fire")
	g.Expect(predicate.Update(ipUpdateEvent(poolWith(0, true), poolWith(1, false)))).
		To(gomega.BeTrue(), "only the 0 -> positive free transition wakes claims")
}

func TestPrefixPoolNoLongerEmptyPredicate(t *testing.T) {
	g := gomega.NewWithT(t)
	predicate := prefixPoolNoLongerEmpty()

	poolWith := func(free int) *v1alpha2.InClusterPrefixPool {
		return &v1alpha2.InClusterPrefixPool{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Status: v1alpha2.InClusterPrefixPoolStatus{
				Addresses: &v1alpha2.InClusterPrefixPoolStatusAddresses{Free: free},
			},
		}
	}

	g.Expect(predicate.Update(ipUpdateEvent(poolWith(0), poolWith(0)))).To(gomega.BeFalse())
	g.Expect(predicate.Update(ipUpdateEvent(poolWith(0), poolWith(1)))).To(gomega.BeTrue())
}
