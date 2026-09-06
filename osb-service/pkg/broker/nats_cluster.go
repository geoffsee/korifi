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

const natsImage = "docker.io/library/nats:2.11.4-alpine"

func (c *vclusterClient) provisionNATS(ctx context.Context, instanceID string) (map[string]any, error) {
	name, err := resourceName("n", instanceID)
	if err != nil {
		return nil, err
	}
	user, err := resourceName("u", instanceID)
	if err != nil {
		return nil, err
	}
	password, err := randomPassword(24)
	if err != nil {
		return nil, err
	}
	host := c.syncedHost("", name, "nats")
	tls, err := generateTLS(host, []string{
		host,
		name + "-nats",
		name + "-nats." + c.namespace,
		name + "-nats." + c.namespace + ".svc",
		name + "-nats." + c.namespace + ".svc.cluster.local",
		name + "-nats-0." + name + "-nats",
	})
	if err != nil {
		return nil, err
	}
	ls := instanceLabels(instanceID, "nats")
	ls["app"] = "nats"
	ls["osb.korifi/cluster"] = name

	if err := c.createTLSSecret(ctx, name+"-tls", ls, tls); err != nil {
		return nil, err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-config", Namespace: c.namespace, Labels: ls},
		Data: map[string]string{
			"nats.conf": fmt.Sprintf(`listen: 0.0.0.0:4222
http: 8222
server_name: %s
jetstream {
  store_dir: /data
  max_mem: 64M
  max_file: 1G
}
tls {
  cert_file: "/etc/nats/tls/tls.crt"
  key_file: "/etc/nats/tls/tls.key"
}
authorization {
  user: %q
  password: %q
}
`, name, user, password),
		},
	}
	if _, err := c.core.CoreV1().ConfigMaps(c.namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create nats config: %w", err)
	}

	port := int32(4222)
	svcLabels := cloneLabels(ls)
	svcLabels["component"] = "nats"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-nats", Namespace: c.namespace, Labels: svcLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"osb.korifi/cluster": name, "component": "nats"},
			Ports:    []corev1.ServicePort{{Name: "nats", Port: port, TargetPort: intstr.FromInt32(port)}},
		},
	}
	if _, err := c.core.CoreV1().Services(c.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create nats service: %w", err)
	}

	replicas := int32(1)
	cpu := resource.MustParse("250m")
	mem := resource.MustParse("512Mi")
	size := resource.MustParse("2Gi")
	fsGroup := int64(1000)
	podLabels := cloneLabels(ls)
	podLabels["component"] = "nats"
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-nats", Namespace: c.namespace, Labels: podLabels},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name + "-nats",
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"osb.korifi/cluster": name, "component": "nats"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{FSGroup: &fsGroup, RunAsNonRoot: boolPtr(true), RunAsUser: int64Ptr(1000)},
					Containers: []corev1.Container{{
						Name:            "nats",
						Image:           natsImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Args:            []string{"--config", "/etc/nats/nats.conf"},
						Ports:           []corev1.ContainerPort{{ContainerPort: port}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/data"},
							{Name: "config", MountPath: "/etc/nats", ReadOnly: true},
							{Name: "tls", MountPath: "/etc/nats/tls", ReadOnly: true},
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)}},
							InitialDelaySeconds: 10,
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             boolPtr(true),
							RunAsUser:                int64Ptr(1000),
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
		return nil, fmt.Errorf("create nats statefulset: %w", err)
	}
	if err := c.waitSTS(ctx, name+"-nats"); err != nil {
		return nil, err
	}
	creds := natsCredentials(host, 4222, user, password)
	creds["ca_cert"] = string(tls.CertPEM)
	creds["cluster"] = name
	creds["instance_id"] = instanceID
	creds["engine"] = "nats"
	return creds, nil
}
