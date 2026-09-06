package broker

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const redisImage = "docker.io/library/redis:7.4.2-alpine"

func (c *vclusterClient) provisionRedis(ctx context.Context, instanceID string) (map[string]any, error) {
	name, err := resourceName("r", instanceID)
	if err != nil {
		return nil, err
	}
	password, err := randomPassword(24)
	if err != nil {
		return nil, err
	}
	host := c.syncedHost("", name, "redis")
	tls, err := generateTLS(host, []string{
		host,
		name + "-redis",
		name + "-redis." + c.namespace,
		name + "-redis." + c.namespace + ".svc",
		name + "-redis." + c.namespace + ".svc.cluster.local",
		name + "-redis-0." + name + "-redis",
	})
	if err != nil {
		return nil, err
	}
	ls := instanceLabels(instanceID, "redis")
	ls["app"] = "redis"
	ls["osb.korifi/cluster"] = name

	if err := c.createTLSSecret(ctx, name+"-tls", ls, tls); err != nil {
		return nil, err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-config", Namespace: c.namespace, Labels: ls},
		Data: map[string]string{
			"redis.conf": fmt.Sprintf(`bind 0.0.0.0
protected-mode yes
port 0
tls-port 6379
tls-cert-file /tls/tls.crt
tls-key-file /tls/tls.key
tls-ca-cert-file /tls/tls.crt
tls-auth-clients no
requirepass %s
appendonly yes
dir /data
`, password),
		},
	}
	if _, err := c.core.CoreV1().ConfigMaps(c.namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create redis config: %w", err)
	}

	port := int32(6379)
	svcLabels := cloneLabels(ls)
	svcLabels["component"] = "redis"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-redis", Namespace: c.namespace, Labels: svcLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"osb.korifi/cluster": name, "component": "redis"},
			Ports:    []corev1.ServicePort{{Name: "redis", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}
	if _, err := c.core.CoreV1().Services(c.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create redis service: %w", err)
	}

	replicas := int32(1)
	cpu := resource.MustParse("250m")
	mem := resource.MustParse("512Mi")
	size := resource.MustParse("2Gi")
	fsGroup := int64(999)
	podLabels := cloneLabels(ls)
	podLabels["component"] = "redis"
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-redis", Namespace: c.namespace, Labels: podLabels},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name + "-redis",
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"osb.korifi/cluster": name, "component": "redis"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{FSGroup: &fsGroup, RunAsNonRoot: boolPtr(true), RunAsUser: int64Ptr(999)},
					Containers: []corev1.Container{{
						Name:            "redis",
						Image:           redisImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Args:            []string{"/etc/redis/redis.conf"},
						Ports:           []corev1.ContainerPort{{ContainerPort: port}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/data"},
							{Name: "config", MountPath: "/etc/redis", ReadOnly: true},
							{Name: "tls", MountPath: "/tls", ReadOnly: true},
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)}},
							InitialDelaySeconds: 10,
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             boolPtr(true),
							RunAsUser:                int64Ptr(999),
							AllowPrivilegeEscalation: boolPtr(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name + "-config"}}}},
						{Name: "tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: name + "-tls"}}},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data", Labels: podLabels},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
				},
			}},
		},
	}
	if _, err := c.core.AppsV1().StatefulSets(c.namespace).Create(ctx, sts, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create redis statefulset: %w", err)
	}
	if err := c.waitSTS(ctx, name+"-redis"); err != nil {
		return nil, err
	}
	creds := redisCredentials(host, 6379, password)
	creds["ca_cert"] = string(tls.CertPEM)
	creds["cluster"] = name
	creds["instance_id"] = instanceID
	creds["engine"] = "redis"
	return creds, nil
}
