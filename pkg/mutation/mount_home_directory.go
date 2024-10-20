package mutation

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	rest "k8s.io/client-go/rest"
)

var configMapName = "uid-mapping"
var namespace = "default"

// minLifespanTolerations is a container for mininum lifespan mutation
type mountHomeDirectory struct {
	Logger logrus.FieldLogger
}

// minLifespanTolerations imhdements the podMutator interface
var _ podMutator = (*mountHomeDirectory)(nil)

// Name returns the minLifespanTolerations short name
func (mhd mountHomeDirectory) Name() string {
	return "mount_home_directory"
}

// Mutate returns a new mutated pod according to lifespan tolerations rules
func (mhd mountHomeDirectory) Mutate(pod *corev1.Pod, a *admissionv1.AdmissionRequest) (*corev1.Pod, error) {

	mhd.Logger = mhd.Logger.WithField("mutation", mhd.Name())
	mpod := pod.DeepCopy()
	securityContext := pod.Spec.SecurityContext
	serviceAccount := getServiceAccount(mhd, a)

	if securityContext == nil || securityContext.RunAsUser == nil {
		if serviceAccount != "" {
			logMessage := fmt.Sprintf("No runAsUser rule found, applying default for current ServiceAccount %s", serviceAccount)
			mhd.Logger.Info(logMessage)

			var err error
			mpod.Spec.SecurityContext, err = setUID(mhd, mpod.Spec.SecurityContext, serviceAccount)
			if err != nil {
				return nil, fmt.Errorf("Failed to set RunAsUser: %s\n", err)
			}
		}
	}
	return mpod, nil
}

// Set RunAsUser field based on ServiceAccountName
func setUID(mhd mountHomeDirectory, existing *corev1.PodSecurityContext, serviceAccount string) (*corev1.PodSecurityContext, error) {
	client, err := initClient()
	if err != nil {
		logMessage := fmt.Sprintf("Failed initializing Kubernetes client: %s\n", err)
		return nil, fmt.Errorf(logMessage)
	}
	configMap, err := getConfigMap(client)
	if err != nil {
		logMessage := fmt.Sprintf("Failed setting UID: %s\n", err)
		return nil, fmt.Errorf(logMessage)
	}
	data := configMap.Data
	uid := data[serviceAccount]

	if uid == "" {
		logMessage := fmt.Sprintf("ServiceAccount %s\n has no UID associated with it", err)
		return nil, fmt.Errorf(logMessage)
	}
	logMessage := fmt.Sprintf("ServiceAccount %s has UID %s associated with it", serviceAccount, uid)
	mhd.Logger.Info(logMessage)
	uid64, err := strconv.ParseInt(data[serviceAccount], 10, 64)

	if err != nil {
		logMessage := fmt.Sprintf("Failed to convert UID to int64: %s", err)
		return nil, fmt.Errorf(logMessage)
	}
	existing.RunAsUser = &uid64
	return existing, nil
}

// Init Kubernetes Client to interact with the API
func initClient() (*kubernetes.Clientset, error) {
	// Init client from inside pod
	config, err := rest.InClusterConfig()
	if err != nil {
		logMessage := fmt.Sprintf("Error getting in-cluster config: %s\n", err)
		return nil, fmt.Errorf(logMessage)
	}

	// Creating client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logMessage := fmt.Sprintf("Error starting Kubernetes client from config: %s\n", err)
		return nil, fmt.Errorf(logMessage)
	}
	return clientset, nil
}

// Retrieve ConfigMap based on name and namespaces
func getConfigMap(client *kubernetes.Clientset) (*corev1.ConfigMap, error) {
	// Get ConfigMap
	configMap, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
	if err != nil {
		logMessage := fmt.Sprintf("Error getting ConfigMap: %s\n", err)
		return nil, fmt.Errorf(logMessage)
	}
	return configMap, nil
}

// Get ServiceAccount from API request
func getServiceAccount(mhd mountHomeDirectory, request *admissionv1.AdmissionRequest) string {
	userInfo := request.UserInfo
	if userInfo.Username != "" && strings.HasPrefix(userInfo.Username, "system:serviceaccount:") {
		parts := strings.Split(userInfo.Username, ":")
		if len(parts) == 4 {
			namespace := parts[2]
			serviceAccountName := parts[3]
			logMessage := fmt.Sprintf("Request made by ServiceAccount: %s in namespace: %s", serviceAccountName, namespace)
			mhd.Logger.Info(logMessage)

			return serviceAccountName
		}
	}
	return ""
}
