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

package controllers

import (
	"context"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/node/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
	"time"

	alpha1 "github.com/zisefeizhu/node-maintain-operator/api/v2alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeMaintainReconciler reconciles a NodeMaintain object
type NodeMaintainReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

const nodeFinalizer = "node.finalizers.node-pool.lailin.xyz"

//+kubebuilder:rbac:groups=batch.node.maintain,resources=nodemaintains,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.node.maintain,resources=nodemaintains/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.node.maintain,resources=nodemaintains/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodeMaintain object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *NodeMaintainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//_ = log.FromContext(ctx)

	// your logic here

	log := r.Log.WithValues("nodeMaintain", req.NamespacedName)

	forget := reconcile.Result{}
	requeue := ctrl.Result{
		RequeueAfter: time.Second * 2,
	}
	// 实例化数据结构
	instance := &alpha1.NodeMaintain{}

	// 通过客户端工具查询，查询条件是
	err := r.Get(ctx, req.NamespacedName, instance)
	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "Error", "some error")
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("instance not found, maybe removed")
			return forget, nil
		}
		log.Error(err, "error")
		return requeue, nil
	}

	var nodes corev1.NodeList

	// 查看是否存在对应的节点，如果存在那么就给这些节点加上数据
	err = r.List(ctx, &nodes, &client.ListOptions{LabelSelector: instance.NodeLabelSelector()})
	if client.IgnoreNotFound(err) != nil {
		return requeue, err
	}

	// 进入预删除流程
	if !instance.DeletionTimestamp.IsZero() {
		return requeue, r.nodeFinalizer(ctx, instance, nodes.Items)
	}

	// 如果珊瑚时间戳为空 说明现在不需要删除该数据，我们将nodeFinalizer 加入到资源中
	if !containsString(instance.Finalizers, nodeFinalizer) {
		if err := r.Client.Update(ctx, instance); err != nil {
			return requeue, err
		}
	}

	// 节点存在
	if len(nodes.Items) > 0 {
		log.Info("find nodes, will merge data", "nodes", len(nodes.Items))
		instance.Status.Allocatable = corev1.ResourceList{}
		instance.Status.NodeCount = len(nodes.Items)
		var NotReady int
		for _, node := range nodes.Items {
			// 非正常运行的节点数
			if nodeNotReady(node.Status) {
				NotReady++
			}
			// 更新节点的标签和污点信息
			err := r.Update(ctx, instance.Spec.ApplyNode(node))
			if err != nil {
				return requeue, err
			}

			for name, quantity := range node.Status.Allocatable {
				q, ok := instance.Status.Allocatable[name]
				if ok {
					q.Add(quantity)
					instance.Status.Allocatable[name] = q
					continue
				}
				instance.Status.Allocatable[name] = quantity
			}
		}
		instance.Status.NotReadyNodeCount = NotReady
	}

	runtimeClass := &v1beta1.RuntimeClass{}
	err = r.Get(ctx, client.ObjectKeyFromObject(instance.RuntimeClass()), runtimeClass)
	if client.IgnoreNotFound(err) != nil {
		return requeue, err
	}

	// 如果不存在创建一个新的
	if runtimeClass.Name == "" {
		runtimeClass := instance.RuntimeClass()
		err = controllerutil.SetOwnerReference(instance, runtimeClass, r.Scheme)
		if err != nil {
			return requeue, err
		}
		err = r.Create(ctx, runtimeClass)
		return requeue, err
	}

	// 如果存在则更新
	runtimeClass.Scheduling = instance.RuntimeClass().Scheduling
	runtimeClass.Handler = instance.RuntimeClass().Handler
	err = r.Client.Update(ctx, runtimeClass)
	if err != nil {
		return requeue, err
	}

	instance.Status.Status = "Runnging"
	err = r.Status().Update(ctx, instance)
	return ctrl.Result{}, err
}

//// SetupWithManager sets up the controller with the Manager.
//func (r *NodeMaintainReconciler) SetupWithManager(mgr ctrl.Manager) error {
//	return ctrl.NewControllerManagedBy(mgr).
//		For(&alpha1.NodeMaintain{}).
//		Complete(r)
//}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeMaintainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&alpha1.NodeMaintain{}).
		Watches(&source.Kind{Type: &corev1.Node{}}, handler.Funcs{UpdateFunc: r.nodeUpdateHandler}).
		Complete(r)
}

func (r *NodeMaintainReconciler) nodeUpdateHandler(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	oldPool, err := r.getNodePoolByLabels(ctx, e.ObjectOld.GetLabels())
	if err != nil {
		r.Log.Error(err, "get node pool err")
	}
	if oldPool != nil {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: oldPool.Name},
		})
	}

	newPool, err := r.getNodePoolByLabels(ctx, e.ObjectNew.GetLabels())
	if err != nil {
		r.Log.Error(err, "get node pool err")
	}
	if newPool != nil {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{Name: newPool.Name},
		})
	}
}

func (r *NodeMaintainReconciler) getNodePoolByLabels(ctx context.Context, labels map[string]string) (*alpha1.NodeMaintain, error) {
	pool := &alpha1.NodeMaintain{}
	for k := range labels {
		ss := strings.Split(k, "node-role.kubernetes.io/")
		if len(ss) != 2 {
			continue
		}
		err := r.Client.Get(ctx, types.NamespacedName{Name: ss[1]}, pool)
		if err == nil {
			return pool, nil
		}

		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
	}
	return nil, nil
}

// 节点预删除逻辑
func (r *NodeMaintainReconciler) nodeFinalizer(ctx context.Context, instance *alpha1.NodeMaintain, nodes []corev1.Node) error {
	// 不为空就说明进入到预删除流程
	for _, n := range nodes {
		n := n

		// 更新节点的标签和污点信息
		err := r.Update(ctx, instance.Spec.CleanNode(n))
		if err != nil {
			return err
		}
	}

	// 预删除执行完毕，移除 nodeFinalizer
	instance.Finalizers = removeString(instance.Finalizers, nodeFinalizer)
	return r.Client.Update(ctx, instance)
}

func nodeNotReady(status corev1.NodeStatus) bool {
	for _, condition := range status.Conditions {
		if condition.Status == "True" && condition.Type == "Ready" {
			return false
		}
	}
	return true
}

// 辅助函数用于检查并从字符串切片中删除字符串。
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// 移除
func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
