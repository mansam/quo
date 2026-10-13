package capture

import (
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// Objects the fixture VirtualMachine refers to.
const (
	testInstancetype = "large-memory"
	testPreference   = "linux-efi"
	testRevision     = "db-01-large-memory-1"
	testNetwork      = "storage-net"
	testSecret       = "db-01-cloudinit"
	testConfigMap    = "db-01-sysprep"
)

func TestExportCapturesEveryObjectTheVirtualMachineReferences(t *testing.T) {
	tests := []struct {
		name string
		// refer makes the fixture VirtualMachine refer to something.
		refer func(t *testing.T, vm *unstructured.Unstructured)
		want  []cluster.ObjectRef
	}{
		{
			name: "a volume naming a PersistentVolumeClaim",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "rootdisk", map[string]any{
					"persistentVolumeClaim": map[string]any{"claimName": rootClaim},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindPersistentVolumeClaim, rootClaim)},
		},
		{
			name: "a volume provisioned by a DataVolume, and the claim it provisions",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "rootdisk", map[string]any{
					"dataVolume": map[string]any{"name": rootClaim},
				})
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindDataVolume, rootClaim),
				ref(cluster.KindPersistentVolumeClaim, rootClaim),
			},
		},
		{
			name: "a DataVolume the VirtualMachine templates, and the claim it provisions",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, []any{
					map[string]any{"metadata": map[string]any{"name": rootClaim}},
				}, "spec", "dataVolumeTemplates")
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindDataVolume, rootClaim),
				ref(cluster.KindPersistentVolumeClaim, rootClaim),
			},
		},
		{
			name: "a volume holding a memory dump",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "memorydump", map[string]any{
					"memoryDump": map[string]any{"claimName": rootClaim},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindPersistentVolumeClaim, rootClaim)},
		},
		{
			name: "an ephemeral volume, whose writes are discarded but whose claim is real",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "rootdisk", map[string]any{
					"ephemeral": map[string]any{
						"persistentVolumeClaim": map[string]any{"claimName": rootClaim},
					},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindPersistentVolumeClaim, rootClaim)},
		},
		{
			name: "a volume holding a ConfigMap",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "config", map[string]any{
					"configMap": map[string]any{"name": testConfigMap},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindConfigMap, testConfigMap)},
		},
		{
			name: "a volume holding a Secret",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "credentials", map[string]any{
					"secret": map[string]any{"secretName": testSecret},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindSecret, testSecret)},
		},
		{
			name: "a volume projecting a ServiceAccount",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "serviceaccount", map[string]any{
					"serviceAccount": map[string]any{"serviceAccountName": "db-01-reader"},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindServiceAccount, "db-01-reader")},
		},
		{
			name: "the Secrets behind a cloud-init NoCloud volume",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "cloudinit", map[string]any{
					"cloudInitNoCloud": map[string]any{
						"secretRef":            map[string]any{"name": testSecret},
						"networkDataSecretRef": map[string]any{"name": "db-01-netdata"},
					},
				})
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindSecret, testSecret),
				ref(cluster.KindSecret, "db-01-netdata"),
			},
		},
		{
			name: "the Secrets behind a cloud-init ConfigDrive volume",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "cloudinit", map[string]any{
					"cloudInitConfigDrive": map[string]any{
						"secretRef":            map[string]any{"name": testSecret},
						"networkDataSecretRef": map[string]any{"name": "db-01-netdata"},
					},
				})
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindSecret, testSecret),
				ref(cluster.KindSecret, "db-01-netdata"),
			},
		},
		{
			name: "the ConfigMap holding a sysprep answer file",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "sysprep", map[string]any{
					"sysprep": map[string]any{
						"configMap": map[string]any{"name": testConfigMap},
					},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindConfigMap, testConfigMap)},
		},
		{
			name: "the Secret holding a sysprep answer file",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "sysprep", map[string]any{
					"sysprep": map[string]any{
						"secret": map[string]any{"name": testSecret},
					},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindSecret, testSecret)},
		},
		{
			name: "the Secret a container disk is pulled with",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "containerdisk", map[string]any{
					"containerDisk": map[string]any{
						"image":           "quay.io/example/disk:latest",
						"imagePullSecret": "registry-credentials",
					},
				})
			},
			want: []cluster.ObjectRef{ref(cluster.KindSecret, "registry-credentials")},
		},
		{
			name: "the Secret holding an injected SSH public key",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, []any{
					map[string]any{
						"sshPublicKey": map[string]any{
							"source": map[string]any{
								"secret": map[string]any{"secretName": "db-01-keys"},
							},
						},
					},
				}, "spec", "template", "spec", "accessCredentials")
			},
			want: []cluster.ObjectRef{ref(cluster.KindSecret, "db-01-keys")},
		},
		{
			name: "the Secret holding an injected user password",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, []any{
					map[string]any{
						"userPassword": map[string]any{
							"source": map[string]any{
								"secret": map[string]any{"secretName": "db-01-passwords"},
							},
						},
					},
				}, "spec", "template", "spec", "accessCredentials")
			},
			want: []cluster.ObjectRef{ref(cluster.KindSecret, "db-01-passwords")},
		},
		{
			name: "the NetworkAttachmentDefinition a Multus network names",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				attachNetwork(t, vm, testNetwork)
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindNetworkAttachmentDefinition, testNetwork),
			},
		},
		{
			name: "a NetworkAttachmentDefinition borrowed from another namespace",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				attachNetwork(t, vm, "shared-networks/"+testNetwork)
			},
			want: []cluster.ObjectRef{
				refIn(cluster.KindNetworkAttachmentDefinition, "shared-networks", testNetwork),
			},
		},
		{
			name: "a namespaced instancetype and the ControllerRevision it was pinned to",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{
					"kind":         cluster.KindVirtualMachineInstancetype,
					"name":         testInstancetype,
					"revisionName": testRevision,
				}, "spec", "instancetype")
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindVirtualMachineInstancetype, testInstancetype),
				ref(cluster.KindControllerRevision, testRevision),
			},
		},
		{
			// KubeVirt reads a matcher with no kind as the cluster-scoped one.
			name: "an instancetype matcher that names no kind",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{"name": testInstancetype}, "spec", "instancetype")
			},
			want: []cluster.ObjectRef{
				clusterRef(cluster.KindVirtualMachineClusterInstancetype, testInstancetype),
			},
		},
		{
			// KubeVirt accepts the resource name in place of the kind.
			name: "an instancetype matcher naming the plural resource rather than the kind",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{
					"kind": "virtualmachineinstancetypes",
					"name": testInstancetype,
				}, "spec", "instancetype")
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindVirtualMachineInstancetype, testInstancetype),
			},
		},
		{
			name: "a namespaced preference and the ControllerRevision it was pinned to",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{
					"kind":         cluster.KindVirtualMachinePreference,
					"name":         testPreference,
					"revisionName": testRevision,
				}, "spec", "preference")
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindVirtualMachinePreference, testPreference),
				ref(cluster.KindControllerRevision, testRevision),
			},
		},
		{
			name: "a preference matcher that names no kind",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{"name": testPreference}, "spec", "preference")
			},
			want: []cluster.ObjectRef{
				clusterRef(cluster.KindVirtualMachineClusterPreference, testPreference),
			},
		},
		{
			name: "the instancetype reference the VirtualMachine's status resolved to",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{
					"kind":                  cluster.KindVirtualMachineInstancetype,
					"name":                  testInstancetype,
					"controllerRevisionRef": map[string]any{"name": testRevision},
				}, "status", "instancetypeRef")
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindVirtualMachineInstancetype, testInstancetype),
				ref(cluster.KindControllerRevision, testRevision),
			},
		},
		{
			name: "the preference reference the VirtualMachine's status resolved to",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, map[string]any{
					"kind":                  cluster.KindVirtualMachinePreference,
					"name":                  testPreference,
					"controllerRevisionRef": map[string]any{"name": testRevision},
				}, "status", "preferenceRef")
			},
			want: []cluster.ObjectRef{
				ref(cluster.KindVirtualMachinePreference, testPreference),
				ref(cluster.KindControllerRevision, testRevision),
			},
		},
		{
			name: "a container disk, which is a requirement rather than an object to carry",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "containerdisk", map[string]any{
					"containerDisk": map[string]any{"image": "quay.io/example/disk:latest"},
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := closureCluster(t, tt.refer, tt.want)

			_, content := captureWithDisks(t, c, testOptions())

			manifest := capturedManifest(t, content)
			g.Expect(collected(manifest)).To(gomega.Equal(append(
				append([]cluster.ObjectRef{ref(cluster.KindVirtualMachine, testName)}, tt.want...),
				sourceCluster()...)))
			for _, want := range tt.want {
				g.Expect(content).To(gomega.HaveKey(bundle.ResourcePath(want.Kind, want.Name)))
			}
		})
	}
}

func TestExportCapturesTheControllerRevisionOfAnInstancetypeThatHasBeenDeleted(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, map[string]any{
		"kind":         cluster.KindVirtualMachineInstancetype,
		"name":         testInstancetype,
		"revisionName": testRevision,
	}, "spec", "instancetype")
	// The cluster holds the ControllerRevision but not the instancetype it
	// was taken from.
	c.hold(ref(cluster.KindControllerRevision, testRevision), nil)

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// The ControllerRevision is now the only complete record of what the
	// VirtualMachine was created with, so the capture must not fail over the
	// instancetype being gone.
	g.Expect(collected(manifest)).To(gomega.Equal(append(
		[]cluster.ObjectRef{
			ref(cluster.KindVirtualMachine, testName),
			ref(cluster.KindControllerRevision, testRevision),
		},
		sourceCluster()...)))
	g.Expect(content).To(gomega.HaveKey(bundle.ResourcePath(cluster.KindControllerRevision, testRevision)))
}

func TestExportCarriesAnObjectReferencedFromMoreThanOnePlaceOnce(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{
			"name": "cloudinit",
			"cloudInitNoCloud": map[string]any{
				"secretRef": map[string]any{"name": testSecret},
			},
		},
		map[string]any{
			"name":   "credentials",
			"secret": map[string]any{"secretName": testSecret},
		},
	}, "spec", "template", "spec", "volumes")
	setField(t, c.vm, []any{
		map[string]any{
			"sshPublicKey": map[string]any{
				"source": map[string]any{
					"secret": map[string]any{"secretName": testSecret},
				},
			},
		},
	}, "spec", "template", "spec", "accessCredentials")
	c.hold(ref(cluster.KindSecret, testSecret), nil)

	order, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	g.Expect(collected(manifest)).To(gomega.Equal(append(
		[]cluster.ObjectRef{
			ref(cluster.KindVirtualMachine, testName),
			ref(cluster.KindSecret, testSecret),
		},
		sourceCluster()...)))
	// Read once from the cluster and written once into the bundle.
	g.Expect(c.requested).To(gomega.Equal([]cluster.ObjectRef{ref(cluster.KindSecret, testSecret)}))
	g.Expect(order).To(gomega.Equal(withSourceCluster([]string{
		bundle.ManifestPath,
		testVMMember,
		bundle.ResourcePath(cluster.KindSecret, testSecret),
	})))
}

func TestExportRefusesTwoObjectsOfOneKindAndNameFromDifferentNamespaces(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{
			"name":   "left",
			"multus": map[string]any{"networkName": "left-networks/" + testNetwork},
		},
		map[string]any{
			"name":   "right",
			"multus": map[string]any{"networkName": "right-networks/" + testNetwork},
		},
	}, "spec", "template", "spec", "networks")
	c.hold(refIn(cluster.KindNetworkAttachmentDefinition, "left-networks", testNetwork), nil)
	c.hold(refIn(cluster.KindNetworkAttachmentDefinition, "right-networks", testNetwork), nil)

	captured, err := captureInto(t, c, testOptions())

	// A member path holds a kind and a name but no namespace, so these two
	// cannot both be carried. Saying so beats writing a bundle whose
	// manifest disagrees with its own members.
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("left-networks/" + testNetwork))
	g.Expect(err.Error()).To(gomega.ContainSubstring("right-networks/" + testNetwork))
	g.Expect(captured).To(gomega.BeEmpty())
}

func TestExportCarriesOnlyTheVirtualMachineWhenItsClosureIsEmpty(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)

	order, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	g.Expect(collected(manifest)).To(gomega.Equal(append(
		[]cluster.ObjectRef{ref(cluster.KindVirtualMachine, testName)},
		sourceCluster()...)))
	// What describes the source cluster is collected whether or not the
	// VirtualMachine refers to anything.
	g.Expect(order).To(gomega.Equal(withSourceCluster([]string{bundle.ManifestPath, testVMMember})))
	g.Expect(c.requested).To(gomega.BeEmpty())
}

func TestExportSeeksNoVirtualMachineInstanceAndNoPod(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := referentialCluster(t)

	_, _ = captureWithDisks(t, c, testOptions())

	// A stopped VirtualMachine has neither, so looking for them would report
	// an absence that means nothing.
	var kinds []string
	for _, requested := range c.requested {
		kinds = append(kinds, requested.Kind)
	}
	g.Expect(kinds).ToNot(gomega.BeEmpty())
	g.Expect(kinds).ToNot(gomega.ContainElement("VirtualMachineInstance"))
	g.Expect(kinds).ToNot(gomega.ContainElement("Pod"))
}

func TestExportCarriesSecretsInFull(t *testing.T) {
	tests := []struct {
		name string
		// refer makes the fixture VirtualMachine refer to the Secret.
		refer  func(t *testing.T, vm *unstructured.Unstructured)
		secret string
		key    string
		value  string
	}{
		{
			name: "cloud-init user data is carried",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "cloudinit", map[string]any{
					"cloudInitNoCloud": map[string]any{
						"secretRef": map[string]any{"name": testSecret},
					},
				})
			},
			secret: testSecret,
			key:    "userdata",
			value:  "I2Nsb3VkLWNvbmZpZwo=",
		},
		{
			name: "a sysprep answer file is carried",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				backVolume(t, vm, "sysprep", map[string]any{
					"sysprep": map[string]any{
						"secret": map[string]any{"name": "db-01-sysprep-secret"},
					},
				})
			},
			secret: "db-01-sysprep-secret",
			key:    "autounattend.xml",
			value:  "PHVuYXR0ZW5kLz4=",
		},
		{
			name: "an injected SSH public key is carried",
			refer: func(t *testing.T, vm *unstructured.Unstructured) {
				setField(t, vm, []any{
					map[string]any{
						"sshPublicKey": map[string]any{
							"source": map[string]any{
								"secret": map[string]any{"secretName": "db-01-keys"},
							},
						},
					},
				}, "spec", "template", "spec", "accessCredentials")
			},
			secret: "db-01-keys",
			key:    "key1",
			value:  "c3NoLWVkMjU1MTkgQUFBQQ==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := stoppedCluster(t)
			tt.refer(t, c.vm)
			c.hold(ref(cluster.KindSecret, tt.secret), map[string]any{
				"type": "Opaque",
				"data": map[string]any{tt.key: tt.value},
			})

			_, content := captureWithDisks(t, c, testOptions())

			object := decodeMember(t, content, bundle.ResourcePath(cluster.KindSecret, tt.secret))
			data := nested(t, object, "data")
			g.Expect(data[tt.key]).To(gomega.Equal(tt.value))
		})
	}
}

func TestExportCarriesObjectsAsKubernetesYAMLThatCanBeAppliedByHand(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := referentialCluster(t)

	order, content := captureWithDisks(t, c, testOptions())

	member := bundle.ResourcePath(cluster.KindConfigMap, testConfigMap)
	g.Expect(order).To(gomega.ContainElement(member))
	// Everything carried sits under the objects grouping, named by its kind.
	g.Expect(member).To(gomega.HavePrefix(bundle.ResourcesDir + "/"))
	object := decodeMember(t, content, member)
	g.Expect(object["apiVersion"]).To(gomega.Equal("v1"))
	g.Expect(object["kind"]).To(gomega.Equal(cluster.KindConfigMap))
	metadata := nested(t, object, "metadata")
	g.Expect(metadata["name"]).To(gomega.Equal(testConfigMap))
	g.Expect(metadata["namespace"]).To(gomega.Equal(testNamespace))
}

// closureCluster builds a cluster whose stopped VirtualMachine refers to the
// given objects and which holds every one of them.
func closureCluster(
	t *testing.T,
	refer func(t *testing.T, vm *unstructured.Unstructured),
	held []cluster.ObjectRef) (c *fakeCluster) {
	//
	t.Helper()
	c = stoppedCluster(t)
	refer(t, c.vm)
	for _, object := range held {
		c.hold(object, nil)
		if object.Kind != cluster.KindPersistentVolumeClaim {
			continue
		}
		c.offer(object.Name)
	}
	return
}

// referentialCluster builds a cluster whose stopped VirtualMachine refers to
// something of every kind quo traverses, all of which the cluster holds.
func referentialCluster(t *testing.T) (c *fakeCluster) {
	t.Helper()
	c = stoppedCluster(t)
	setField(t, c.vm, map[string]any{
		"kind":         cluster.KindVirtualMachineInstancetype,
		"name":         testInstancetype,
		"revisionName": testRevision,
	}, "spec", "instancetype")
	setField(t, c.vm, map[string]any{"name": testPreference}, "spec", "preference")
	setField(t, c.vm, []any{
		map[string]any{
			"name":                  "rootdisk",
			"persistentVolumeClaim": map[string]any{"claimName": rootClaim},
		},
		map[string]any{
			"name":       "datadisk",
			"dataVolume": map[string]any{"name": dataClaim},
		},
		map[string]any{
			"name":    "sysprep",
			"sysprep": map[string]any{"configMap": map[string]any{"name": testConfigMap}},
		},
		map[string]any{
			"name": "cloudinit",
			"cloudInitNoCloud": map[string]any{
				"secretRef": map[string]any{"name": testSecret},
			},
		},
		map[string]any{
			"name":           "serviceaccount",
			"serviceAccount": map[string]any{"serviceAccountName": "db-01-reader"},
		},
	}, "spec", "template", "spec", "volumes")
	setField(t, c.vm, []any{
		map[string]any{
			"sshPublicKey": map[string]any{
				"source": map[string]any{
					"secret": map[string]any{"secretName": "db-01-keys"},
				},
			},
		},
	}, "spec", "template", "spec", "accessCredentials")
	attachNetwork(t, c.vm, testNetwork)
	for _, object := range []cluster.ObjectRef{
		ref(cluster.KindVirtualMachineInstancetype, testInstancetype),
		ref(cluster.KindControllerRevision, testRevision),
		clusterRef(cluster.KindVirtualMachineClusterPreference, testPreference),
		ref(cluster.KindPersistentVolumeClaim, rootClaim),
		ref(cluster.KindDataVolume, dataClaim),
		ref(cluster.KindPersistentVolumeClaim, dataClaim),
		ref(cluster.KindConfigMap, testConfigMap),
		ref(cluster.KindSecret, testSecret),
		ref(cluster.KindServiceAccount, "db-01-reader"),
		ref(cluster.KindSecret, "db-01-keys"),
		ref(cluster.KindNetworkAttachmentDefinition, testNetwork),
	} {
		c.hold(object, nil)
	}
	c.offer(rootClaim)
	c.offer(dataClaim)
	return
}

// attachNetwork puts one Multus network on the fixture VirtualMachine.
func attachNetwork(t *testing.T, vm *unstructured.Unstructured, name string) {
	t.Helper()
	setField(t, vm, []any{
		map[string]any{
			"name":   "secondary",
			"multus": map[string]any{"networkName": name},
		},
	}, "spec", "template", "spec", "networks")
}

// collected returns the resources a manifest says the bundle holds.
func collected(manifest *bundle.Manifest) (refs []cluster.ObjectRef) {
	for i := range manifest.Resources {
		object := manifest.Resources[i]
		refs = append(refs, cluster.ObjectRef{
			Kind:      object.Kind,
			Namespace: object.Namespace,
			Name:      object.Name,
		})
	}
	return
}
