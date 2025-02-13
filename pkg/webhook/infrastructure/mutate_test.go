// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package infrastructure

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	azurev1alpha1 "github.com/gardener/gardener-extension-provider-azure/pkg/apis/azure/v1alpha1"
	"github.com/gardener/gardener-extension-provider-azure/pkg/azure"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

const (
	workerCIDR = "10.0.0.0/16"
)

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Infrastructure Webhook Suite")
}

var _ = Describe("Mutate", func() {
	var ctrl *gomock.Controller

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#NetworkLayoutMigration", func() {
		var mutator extensionswebhook.Mutator

		BeforeEach(func() {
			mutator = New(logger, NetworkLayoutMigrationMutate)
		})

		Context("add migration annotation", func() {
			var workersConfig, zonesConfig *azurev1alpha1.InfrastructureConfig

			BeforeEach(func() {
				workersConfig = &azurev1alpha1.InfrastructureConfig{
					TypeMeta: metav1.TypeMeta{
						Kind:       "InfrastructureConfig",
						APIVersion: azurev1alpha1.SchemeGroupVersion.String(),
					},
					Networks: azurev1alpha1.NetworkConfig{
						Workers: pointer.String(workerCIDR),
					},
					Zoned: true,
				}

				zonesConfig = &azurev1alpha1.InfrastructureConfig{
					TypeMeta: metav1.TypeMeta{
						Kind:       "InfrastructureConfig",
						APIVersion: azurev1alpha1.SchemeGroupVersion.String(),
					},
					Networks: azurev1alpha1.NetworkConfig{
						Zones: []azurev1alpha1.Zone{
							{
								Name: int32(1),
								CIDR: "10.11.0.0/16",
							},
							{
								Name: int32(2),
								CIDR: workerCIDR,
							},
						},
					},
					Zoned: true,
				}
			})
			It("should mutate the resource when migrating network layout", func() {
				oldInfra := generateInfrastructureWithProviderConfig(workersConfig, nil)
				newInfra := generateInfrastructureWithProviderConfig(zonesConfig, nil)

				err := mutator.Mutate(context.TODO(), newInfra, oldInfra)

				Expect(err).To(BeNil())
				v, ok := getLayoutMigrationAnnotation(newInfra)
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("2"))
			})
			It("should do nothing if network setup stays the same", func() {
				newInfra := generateInfrastructureWithProviderConfig(workersConfig, nil)

				err := mutator.Mutate(context.TODO(), newInfra, newInfra)

				Expect(err).To(BeNil())
				_, ok := getLayoutMigrationAnnotation(newInfra)
				Expect(ok).To(BeFalse())
			})
			It("should do nothing if it is a create operation", func() {
				newInfra := generateInfrastructureWithProviderConfig(zonesConfig, nil)

				err := mutator.Mutate(context.TODO(), newInfra, nil)

				Expect(err).To(BeNil())
				_, ok := getLayoutMigrationAnnotation(newInfra)
				Expect(ok).To(BeFalse())
			})
			It("should do nothing if network setup stays the same with zonal layout", func() {
				newInfra := generateInfrastructureWithProviderConfig(zonesConfig, nil)

				err := mutator.Mutate(context.TODO(), newInfra, newInfra)

				Expect(err).To(BeNil())
				_, ok := getLayoutMigrationAnnotation(newInfra)
				Expect(ok).To(BeFalse())
			})
		})

		Context("remove migration annotation", func() {
			var (
				migratedSubnet int
				zonesInfra     *extensionsv1alpha1.Infrastructure
				zonesConfig    *azurev1alpha1.InfrastructureConfig
			)

			BeforeEach(func() {
				migratedSubnet = 1

				zonesConfig = &azurev1alpha1.InfrastructureConfig{
					TypeMeta: metav1.TypeMeta{
						Kind:       "InfrastructureConfig",
						APIVersion: azurev1alpha1.SchemeGroupVersion.String(),
					},
					Zoned: true,
					Networks: azurev1alpha1.NetworkConfig{
						Zones: []azurev1alpha1.Zone{
							{
								Name: int32(1),
							},
							{
								Name: int32(2),
							},
						},
					},
				}
				zonesInfra = generateInfrastructureWithProviderConfig(zonesConfig, nil)
				addLayoutMigrationAnnotation(zonesInfra, migratedSubnet)
			})
			It("should remove the annotation when the zone is no longer in use", func() {
				zonesConfig.Networks.Zones = zonesConfig.Networks.Zones[1:]
				newZonesInfra := generateInfrastructureWithProviderConfig(zonesConfig, nil)
				addLayoutMigrationAnnotation(newZonesInfra, migratedSubnet)

				err := mutator.Mutate(context.TODO(), newZonesInfra, zonesInfra)
				Expect(err).To(BeNil())
				_, ok := getLayoutMigrationAnnotation(newZonesInfra)
				Expect(ok).To(BeFalse())
			})
			It("should keep the annotation is the zone is still in use", func() {
				err := mutator.Mutate(context.TODO(), zonesInfra, nil)
				Expect(err).To(BeNil())
				a, ok := getLayoutMigrationAnnotation(zonesInfra)
				Expect(ok).To(BeTrue())
				Expect(a).To(Equal(strconv.Itoa(migratedSubnet)))
			})
		})
	})
})

func generateInfrastructureWithProviderConfig(config *azurev1alpha1.InfrastructureConfig, status *azurev1alpha1.IdentityStatus) *extensionsv1alpha1.Infrastructure {
	infra := &extensionsv1alpha1.Infrastructure{}

	if config != nil {
		marshalled, err := json.Marshal(config)
		Expect(err).To(BeNil())

		infra.Spec.DefaultSpec.ProviderConfig = &runtime.RawExtension{
			Raw: marshalled,
		}
	}

	if status != nil {
		marshalled, err := json.Marshal(status)
		Expect(err).To(BeNil())

		infra.Status.ProviderStatus = &runtime.RawExtension{
			Raw: marshalled,
		}
	}

	return infra
}

func getLayoutMigrationAnnotation(o *extensionsv1alpha1.Infrastructure) (string, bool) {
	return getAnnotation(azure.NetworkLayoutZoneMigrationAnnotation, o)
}

func addLayoutMigrationAnnotation(o *extensionsv1alpha1.Infrastructure, zone int) {
	if o.Annotations == nil {
		o.Annotations = make(map[string]string)
	}
	o.Annotations[azure.NetworkLayoutZoneMigrationAnnotation] = strconv.Itoa(zone)
}

func getAnnotation(anno string, o extensionsv1alpha1.Object) (string, bool) {
	v, ok := o.GetAnnotations()[anno]
	return v, ok
}
