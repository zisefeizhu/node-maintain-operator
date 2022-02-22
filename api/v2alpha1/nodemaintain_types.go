/*
Copyright 2022.

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

package v2alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/node/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"strings"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NodeMaintainSpec defines the desired state of NodeMaintain
type NodeMaintainSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Taints 污点
	Taints []corev1.Taint `json:"taints,omitempty"`

	// Labels 标签
	Labels map[string]string `json:"labels,omitempty"`

	// Handler 对应 Runtime class 的Handler
	Handler string `json:"handler,omitempty"`
}

// CleanNode 清理节点标签污点信息，仅保留系统标签
func (s *NodeMaintainSpec) CleanNode(node corev1.Node) *corev1.Node {
	// 除了节点池的标签之外，只保留k8s的相关标签
	nodeLabels := map[string]string{}
	for k, v := range node.Labels {
		if strings.Contains(k, "kubernetes") {
			nodeLabels[k] = v
		}
	}
	node.Labels = nodeLabels

	// 污点同理
	var taints []corev1.Taint
	for _, taint := range node.Spec.Taints {
		if strings.Contains(taint.Key, "kubernetes") {
			taints = append(taints, taint)
		}
	}
	node.Spec.Taints = taints
	return &node
}

// ApplyNode 生成Node结构，可以用于Patch 数据
func (s *NodeMaintainSpec) ApplyNode(node corev1.Node) *corev1.Node {
	n := s.CleanNode(node)

	for k, v := range s.Labels {
		n.Labels[k] = v
	}

	n.Spec.Taints = append(n.Spec.Taints, s.Taints...)
	return n
}

// NodeMaintainStatus defines the observed state of NodeMaintain
type NodeMaintainStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// status=200 说明正常，其他情况为异常情况
	Status string `json:"status,omitempty"`
	// 节点的数量
	NodeCount int `json:"nodeCount,omitempty"`
	// 未准备好节点计数
	NotReadyNodeCount int `json:"notReadyNodeCount"`
	// 允许被调度的容量
	Allocatable corev1.ResourceList `json:"allocatable,omitempty" protobuf:"bytes,2,rep,name=allocatable,casttype=ResourceList,castkey=ResourceName"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster,shortName={"nm"}
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+kubebuilder:printcolumn:JSONPath=".status.status",name=Status,type=string
//+kubebuilder:printcolumn:JSONPath=".status.nodeCount",name=NodeCount,type=integer
//+kubebuilder:printcolumn:JSONPath=".status.notReadyNodeCount",name=NotReadyNodeCount,type=integer

// NodeMaintain is the Schema for the nodemaintains API
type NodeMaintain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeMaintainSpec   `json:"spec,omitempty"`
	Status NodeMaintainStatus `json:"status,omitempty"`
}

// NodeRole 返回节点对应的 role 标签名
func (n *NodeMaintain) NodeRole() string {
	return "node-role.kubernetes.io/" + n.Name
}

// NodeLabelSelector 返回节点 label 选择器
func (n *NodeMaintain) NodeLabelSelector() labels.Selector {
	return labels.SelectorFromSet(map[string]string{
		n.NodeRole(): "",
	})
}

// RuntimeClass 生成对应的 runtime class 对象
func (n *NodeMaintain) RuntimeClass() *v1beta1.RuntimeClass {
	s := n.Spec
	tolerations := make([]corev1.Toleration, len(s.Taints))
	for i, t := range s.Taints {
		tolerations[i] = corev1.Toleration{
			Key:      t.Key,
			Value:    t.Value,
			Effect:   t.Effect,
			Operator: corev1.TolerationOpEqual,
		}
	}

	return &v1beta1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-pool-" + n.Name,
		},
		Handler: "runc",
		Scheduling: &v1beta1.Scheduling{
			NodeSelector: s.Labels,
			Tolerations:  tolerations,
		},
	}
}

//+kubebuilder:object:root=true

// NodeMaintainList contains a list of NodeMaintain
type NodeMaintainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeMaintain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeMaintain{}, &NodeMaintainList{})
}
