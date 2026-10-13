package cluster

import "k8s.io/apimachinery/pkg/runtime/schema"

// resource is the API resource serving one kind, and whether that resource is
// cluster-scoped.
type resource struct {
	gvr           schema.GroupVersionResource
	clusterScoped bool
}

// resourceByKind maps every kind quo reads by reference onto the API resource
// that serves it. A kind missing from this table is one quo cannot read, which
// is reported rather than guessed at.
var resourceByKind = map[string]resource{
	KindConfigMap: {
		gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
	},
	KindControllerRevision: {
		gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "controllerrevisions"},
	},
	KindDataVolume: {
		gvr: schema.GroupVersionResource{Group: "cdi.kubevirt.io", Version: "v1beta1", Resource: "datavolumes"},
	},
	KindNetworkAttachmentDefinition: {
		gvr: schema.GroupVersionResource{
			Group:    "k8s.cni.cncf.io",
			Version:  "v1",
			Resource: "network-attachment-definitions",
		},
	},
	KindPersistentVolume: {
		gvr:           schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"},
		clusterScoped: true,
	},
	KindPersistentVolumeClaim: {
		gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"},
	},
	KindStorageClass: {
		gvr: schema.GroupVersionResource{
			Group:    "storage.k8s.io",
			Version:  "v1",
			Resource: "storageclasses",
		},
		clusterScoped: true,
	},
	KindStorageProfile: {
		gvr: schema.GroupVersionResource{
			Group:    "cdi.kubevirt.io",
			Version:  "v1beta1",
			Resource: "storageprofiles",
		},
		clusterScoped: true,
	},
	KindSecret: {
		gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
	},
	KindServiceAccount: {
		gvr: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"},
	},
	KindVirtualMachineInstancetype: {
		gvr: instancetypeResource("virtualmachineinstancetypes"),
	},
	KindVirtualMachineClusterInstancetype: {
		gvr:           instancetypeResource("virtualmachineclusterinstancetypes"),
		clusterScoped: true,
	},
	KindVirtualMachinePreference: {
		gvr: instancetypeResource("virtualmachinepreferences"),
	},
	KindVirtualMachineClusterPreference: {
		gvr:           instancetypeResource("virtualmachineclusterpreferences"),
		clusterScoped: true,
	},
}

// instancetypeResource names a resource in KubeVirt's instancetype group.
func instancetypeResource(name string) (gvr schema.GroupVersionResource) {
	gvr = schema.GroupVersionResource{
		Group:    "instancetype.kubevirt.io",
		Version:  "v1beta1",
		Resource: name,
	}
	return
}
