/*
Copyright 2024 huoyujia.

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

package controller

import (
	"context"
	appsv1s "github.com/asjfoajs/kubebuilder-rocketmq/api/v1"
	"github.com/asjfoajs/kubebuilder-rocketmq/internal/constants"
	"github.com/asjfoajs/kubebuilder-rocketmq/internal/share"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"os/exec"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sort"
	"strconv"
	"strings"
	"time"
)

var log = logf.Log.WithName("controller_nameservice")

// NameServiceReconciler reconciles a NameService object
type NameServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.hyj.cn,resources=nameservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.hyj.cn,resources=nameservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.hyj.cn,resources=nameservices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NameService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *NameServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling NameService")

	// Fetch the NameService instance
	instance := &appsv1s.NameService{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Check if the statefulSet already exists, if not create a new one
	found := &appsv1.StatefulSet{}

	//返回一个statefulSet，注意这里并没有创建
	dep := r.statefulSetForNameService(instance)

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		//没有找到就创建
		err = r.Client.Create(context.TODO(), dep)
		if err != nil {
			reqLogger.Error(err, "Failed to create new StatefulSet of NameService", "StatefulSet.Namespace", dep.Namespace, "StatefulSet.Name", dep.Name)
		}
		// StatefulSet created successfully - return and requeue
		return reconcile.Result{Requeue: true}, nil
	} else if err != nil {
		reqLogger.Error(err, "Failed to get NameService StatefulSet.")
	}

	// Ensure the statefulSet size is the same as the spec
	size := instance.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		//数量不够？
		err = r.Client.Update(context.TODO(), found)
		reqLogger.Info("NameService Updated")
		if err != nil {
			reqLogger.Error(err, "Failed to update StatefulSet.", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
			return reconcile.Result{}, err
		}
	}

	return r.updateNameServiceStatus(instance, req, true)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NameServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1s.NameService{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func (r *NameServiceReconciler) statefulSetForNameService(nameService *appsv1s.NameService) *appsv1.StatefulSet {
	//Convert to this structure
	//{"app": "name_service", "name_service_cr": nameService.Name}
	ls := labelsForNameService(nameService.Name)

	//如果没有设置VCT的名字，则随机生成一个,
	//因为只需要一个VCT去挂载log目录的数据所以这里是[0]
	if strings.EqualFold(nameService.Spec.VolumeClaimTemplates[0].Name, "") {
		nameService.Spec.VolumeClaimTemplates[0].Name = uuid.New().String()
	}

	dep := &appsv1.StatefulSet{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      nameService.Name,
			Namespace: nameService.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &nameService.Spec.Size,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: nameService.Spec.ServiceAccountName,
					Affinity:           nameService.Spec.Affinity,
					Tolerations:        nameService.Spec.Tolerations,
					NodeSelector:       nameService.Spec.NodeSelector,
					PriorityClassName:  nameService.Spec.PriorityClassName,
					HostNetwork:        nameService.Spec.HostNetwork,
					DNSPolicy:          nameService.Spec.DNSPolicy,
					ImagePullSecrets:   nameService.Spec.ImagePullSecrets,
					Containers: []corev1.Container{{
						Resources:       nameService.Spec.Resources,
						Image:           nameService.Spec.NameServiceImage,
						Name:            "name-service",
						ImagePullPolicy: nameService.Spec.ImagePullPolicy,
						Env:             nameService.Spec.Env,
						Ports: []corev1.ContainerPort{{
							ContainerPort: constants.NameServiceMainContainerPort,
							Name:          constants.NameServiceMainContainerPortName,
						}},
						VolumeMounts: []corev1.VolumeMount{{
							MountPath: constants.LogMountPath,
							Name:      nameService.Spec.VolumeClaimTemplates[0].Name,
							SubPath:   constants.LogSubPathName,
						}},
						//为 Pod 或容器配置安全上下文
						SecurityContext: getContainerSecurityContext(nameService),
					}},
					Volumes:         getVolumes(nameService),
					SecurityContext: getPodSecurityContext(nameService),
				},
			},
			VolumeClaimTemplates: getVolumeClaimTemplates(nameService),
		},
	}

	//Set Broker instance as the owner and controller
	controllerutil.SetControllerReference(nameService, dep, r.Scheme)

	return dep
}

func (r *NameServiceReconciler) updateNameServiceStatus(instance *appsv1s.NameService, request reconcile.Request, requeue bool) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Check the NameServers status")
	// List the pods for this nameService's statefulSet
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labelsForNameService(instance.Name))
	listOps := &client.ListOptions{
		Namespace:     instance.Namespace,
		LabelSelector: labelSelector,
	}

	err := r.Client.List(context.TODO(), podList, listOps)
	if err != nil {
		reqLogger.Error(err, "Failed to list pods.", "NameService.Namespace", instance.Namespace, "NameService.Name", instance.Name)
		return reconcile.Result{Requeue: true}, err
	}
	//获取所有pod的ip
	hostIps := getNameServers(podList.Items)

	//先按照ip 排序，在列表中排序
	sort.Strings(hostIps)
	sort.Strings(instance.Status.NameServices)

	//生成类似
	//192.168.1.1:9876;192.168.1.2:9876;192.168.1.3:9876;
	nameServerListStr := ""
	for _, value := range hostIps {
		nameServerListStr = nameServerListStr + value + ":9876;"
	}

	// Update status.NameServers if needed
	// 两个数组不相等，需要更新状态
	if !reflect.DeepEqual(hostIps, instance.Status.NameServices) {
		//原来的pod列表
		oldNameServerListStr := ""
		for _, value := range instance.Status.NameServices {
			oldNameServerListStr = oldNameServerListStr + value + ":9876;"
		}

		//从新的复制一份数组并去除;
		share.NameServersStr = nameServerListStr[:len(nameServerListStr)-1]
		reqLogger.Info("share.NameServersStr:" + share.NameServersStr)

		//老的小于8，就是ip不可能小于8只能说明有问题
		if len(oldNameServerListStr) <= constants.MinIpListLength {
			oldNameServerListStr = share.NameServersStr
		} else if len(share.NameServersStr) > constants.MinIpListLength {
			//正常更新状态
			oldNameServerListStr = oldNameServerListStr[:len(oldNameServerListStr)-1]
			share.IsNameServersStrUpdated = true
		}
		reqLogger.Info("oldNameServerListStr:" + oldNameServerListStr)
		//更新状态
		instance.Status.NameServices = hostIps
		err := r.Client.Status().Update(context.TODO(), instance)
		// Update the NameServers status with the host ips
		reqLogger.Info("Updated the NameServers status with the host IP")
		if err != nil {
			reqLogger.Error(err, "Failed to update NameServers status of NameService.")
			return reconcile.Result{Requeue: true}, err
		}

		//use admin tool to update broker config
		//就是将broker的配置文件中关于nameServer的配置更新，同admin的方式
		if share.IsNameServersStrUpdated && (len(oldNameServerListStr) > constants.MinIpListLength) &&
			(len(share.NameServersStr) > constants.MinIpListLength) {

			mqAdmin := constants.AdminToolDir
			subCmd := constants.UpdateBrokerConfig
			key := constants.ParamNameServiceAddress

			reqLogger.Info("share.GroupNum=broker.Spec.Size=" + strconv.Itoa(share.GroupNum))

			clusterName := share.BrokerClusterName
			reqLogger.Info("Updating config " + key + " of cluster" + clusterName)
			command := mqAdmin + " " + subCmd + " -c " + clusterName + " -k " + key + " -n " + oldNameServerListStr + " -v" + share.NameServersStr
			cmd := exec.Command("sh", mqAdmin, subCmd, "-c", clusterName, "-k", key, "-n", oldNameServerListStr, "-v", share.NameServersStr)
			output, err := cmd.Output()
			if err != nil {
				reqLogger.Error(err, "Update Broker config "+key+" failed of cluster "+clusterName+", command: "+command)
				return reconcile.Result{Requeue: true}, err
			}
			reqLogger.Info("Successfully updated Broker config " + key + " of cluster " + clusterName + ", command: " + command + ", with output: " + string(output))
		}
	}

	// Print NameServers IP
	for i, value := range instance.Status.NameServices {
		reqLogger.Info("NameService IP[" + strconv.Itoa(i) + "]: " + value)
	}

	runningNameServerNum := getRunningNameServersNum(podList.Items)
	if runningNameServerNum == instance.Spec.Size {
		share.IsNameServersStrInitialized = true
		share.NameServersStr = nameServerListStr //reassign if operator restarts
	}

	reqLogger.Info("Share variables", "GroupNum", share.GroupNum,
		"NameServersStr", share.NameServersStr, "IsNameServersStrUpdated", share.IsNameServersStrUpdated,
		"IsNameServersStrInitialized", share.IsNameServersStrInitialized, "BrokerClusterName", share.BrokerClusterName)

	if requeue {
		return reconcile.Result{Requeue: true, RequeueAfter: time.Duration(constants.RequeueIntervalInSecond) * time.Second}, nil
	}

	return reconcile.Result{}, nil
}

func labelsForNameService(name string) map[string]string {
	return map[string]string{"app": "name_service", "name_service_cr": name}
}

// getContainerSecurityContext returns the security context for the container
// 不直接赋值而单独抽出一个方法是为了避免如果为空时，返回new的一个空对象，而不是空
func getContainerSecurityContext(nameService *appsv1s.NameService) *corev1.SecurityContext {
	var securityContext = corev1.SecurityContext{}
	if nameService.Spec.ContainerSecurityContext != nil {
		securityContext = *nameService.Spec.ContainerSecurityContext
	}
	return &securityContext
}

// getVolumes 根据存储模式，创建卷
func getVolumes(nameService *appsv1s.NameService) []corev1.Volume {
	switch nameService.Spec.StorageMode {
	//不需要创建卷
	case constants.StorageModeStorageClass:
		return nil
	case constants.StorageModeEmptyDir:
		//创建一个临时
		volumes := []corev1.Volume{{
			Name: nameService.Spec.VolumeClaimTemplates[0].Name,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}}
		return volumes
	case constants.StorageModeHostPath:
		fallthrough
	default:
		//创建一个本地
		volumes := []corev1.Volume{{
			Name: nameService.Spec.VolumeClaimTemplates[0].Name,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: nameService.Spec.HostPath,
				}},
		}}
		return volumes
	}
}

func getPodSecurityContext(nameService *appsv1s.NameService) *corev1.PodSecurityContext {
	var securityContext = corev1.PodSecurityContext{}
	if nameService.Spec.PodSecurityContext != nil {
		securityContext = *nameService.Spec.PodSecurityContext
	}
	return &securityContext
}

// getVolumeClaimTemplates 为StorageClass创建一个VCT
func getVolumeClaimTemplates(nameService *appsv1s.NameService) []corev1.PersistentVolumeClaim {
	switch nameService.Spec.StorageMode {
	case constants.StorageModeStorageClass:
		return nameService.Spec.VolumeClaimTemplates
	case constants.StorageModeEmptyDir, constants.StorageModeHostPath:
		fallthrough
	default:
		return nil
	}
}

func getNameServers(pods []corev1.Pod) []string {
	var nameServers []string
	for _, pod := range pods {
		//pod为运行状态，且IP不为空
		if pod.Status.Phase == corev1.PodRunning && !strings.EqualFold(pod.Status.PodIP, "") {
			//将ip添加到一个string数组中
			nameServers = append(nameServers, pod.Status.PodIP)
		}
	}
	return nameServers
}

func getRunningNameServersNum(pods []corev1.Pod) int32 {
	var num int32 = 0
	for _, pod := range pods {
		if reflect.DeepEqual(pod.Status.Phase, corev1.PodRunning) {
			num++
		}
	}
	return num
}
