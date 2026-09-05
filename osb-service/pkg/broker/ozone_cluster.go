package broker

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const ozoneImage = "docker.io/apache/ozone:2.0.0"

func (c *vclusterClient) provisionOzone(ctx context.Context, instanceID string) (map[string]any, error) {
	name, err := resourceName("o", instanceID)
	if err != nil {
		return nil, err
	}
	access, err := resourceName("a", instanceID)
	if err != nil {
		return nil, err
	}
	secretKey, err := randomPassword(32)
	if err != nil {
		return nil, err
	}
	bucket := name
	host := c.syncedHost("", name, "s3g")
	tls, err := generateTLS(host, []string{
		host,
		name + "-s3g",
		name + "-s3g." + c.namespace,
		name + "-s3g." + c.namespace + ".svc",
		name + "-s3g." + c.namespace + ".svc.cluster.local",
		name + "-s3g-0." + name + "-s3g",
	})
	if err != nil {
		return nil, err
	}

	labels := instanceLabels(instanceID, "ozone")
	labels["app"] = "ozone"
	labels["osb.korifi/cluster"] = name

	if err := c.createOzoneConfig(ctx, name, labels, tls.Password); err != nil {
		return nil, err
	}
	if err := c.createTLSSecret(ctx, name+"-tls", labels, tls); err != nil {
		return nil, err
	}
	if err := c.createCredsSecret(ctx, name+"-creds", labels, access, secretKey); err != nil {
		return nil, err
	}

	components := []struct {
		comp   string
		port   int32
		args   []string
		init   []string
		env    []corev1.EnvVar
		volume bool
	}{
		{comp: "scm", port: 9861, args: []string{"ozone", "scm"}, init: []string{"ozone", "scm", "--init"}, volume: true},
		{
			comp: "om", port: 9862, args: []string{"ozone", "om"}, volume: true,
			env: []corev1.EnvVar{
				{Name: "WAITFOR", Value: name + "-scm-0." + name + "-scm:9876"},
				{Name: "ENSURE_OM_INITIALIZED", Value: "/data/metadata/om/current/VERSION"},
			},
		},
		{comp: "datanode", port: 9882, args: []string{"ozone", "datanode"}, volume: true},
		{
			comp: "s3g", port: 9878, args: []string{"ozone", "s3g"},
			env: []corev1.EnvVar{{Name: "WAITFOR", Value: name + "-om-0." + name + "-om:9862"}},
		},
	}
	for _, spec := range components {
		if err := c.createHeadlessService(ctx, name+"-"+spec.comp, spec.comp, spec.port, labels); err != nil {
			return nil, err
		}
		if err := c.createOzoneSTS(ctx, name, spec.comp, spec.port, spec.args, spec.init, spec.env, spec.volume, labels); err != nil {
			return nil, err
		}
	}
	if err := c.waitSTS(ctx, name+"-s3g"); err != nil {
		return nil, err
	}
	creds := ozoneCredentials(host, 9878, bucket, access, secretKey)
	creds["cluster"] = name
	creds["instance_id"] = instanceID
	creds["engine"] = "ozone"
	return creds, nil
}

func (c *vclusterClient) createOzoneConfig(ctx context.Context, name string, ls map[string]string, keystorePass string) error {
	scm := name + "-scm-0." + name + "-scm"
	om := name + "-om-0." + name + "-om"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-config", Namespace: c.namespace, Labels: ls},
		Data: map[string]string{
			"CORE-SITE.XML_fs.defaultFS":                           "ofs://om/",
			"CORE-SITE.XML_hadoop.proxyuser.hadoop.groups":         "*",
			"CORE-SITE.XML_hadoop.proxyuser.hadoop.hosts":          "*",
			"OZONE-SITE.XML_hdds.datanode.use.datanode.hostname":   "true",
			"OZONE-SITE.XML_hdds.datanode.dir":                     "/data/storage",
			"OZONE-SITE.XML_hdds.scm.safemode.min.datanode":        "1",
			"OZONE-SITE.XML_ozone.datanode.pipeline.limit":         "1",
			"OZONE-SITE.XML_ozone.replication":                     "1",
			"OZONE-SITE.XML_hdds.datanode.volume.min.free.space":   "100MB",
			"OZONE-SITE.XML_ozone.metadata.dirs":                   "/data/metadata",
			"OZONE-SITE.XML_ozone.om.address":                      om,
			"OZONE-SITE.XML_ozone.scm.block.client.address":        scm,
			"OZONE-SITE.XML_ozone.scm.client.address":              scm,
			"OZONE-SITE.XML_ozone.scm.datanode.id.dir":             "/data/metadata",
			"OZONE-SITE.XML_ozone.scm.names":                       scm,
			"OZONE-SITE.XML_ozone.http.policy":                     "HTTPS_ONLY",
			"OZONE-SITE.XML_ozone.s3g.https-address":               "0.0.0.0:9878",
			"SSL-SERVER.XML_ssl.server.keystore.location":          "/etc/ozone-tls/keystore.p12",
			"SSL-SERVER.XML_ssl.server.keystore.password":          keystorePass,
			"SSL-SERVER.XML_ssl.server.keystore.type":              "PKCS12",
			"SSL-SERVER.XML_ssl.server.truststore.location":        "/etc/ozone-tls/keystore.p12",
			"SSL-SERVER.XML_ssl.server.truststore.password":        keystorePass,
			"SSL-SERVER.XML_ssl.server.truststore.type":            "PKCS12",
			"LOG4J.PROPERTIES_log4j.rootLogger":                    "INFO, stdout",
			"LOG4J.PROPERTIES_log4j.appender.stdout":               "org.apache.log4j.ConsoleAppender",
			"LOG4J.PROPERTIES_log4j.appender.stdout.layout":        "org.apache.log4j.PatternLayout",
			"LOG4J.PROPERTIES_log4j.appender.stdout.layout.ConversionPattern": "%d{yyyy-MM-dd HH:mm:ss} %-5p %c{1}:%L - %m%n",
		},
	}
	_, err := c.core.CoreV1().ConfigMaps(c.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create ozone config: %w", err)
	}
	return nil
}

func (c *vclusterClient) createTLSSecret(ctx context.Context, name string, ls map[string]string, tls tlsMaterial) error {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: ls},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       tls.CertPEM,
			corev1.TLSPrivateKeyKey: tls.KeyPEM,
			"keystore.p12":          tls.PKCS12,
			"keystore.password":     []byte(tls.Password),
		},
	}
	_, err := c.core.CoreV1().Secrets(c.namespace).Create(ctx, sec, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create tls secret: %w", err)
	}
	return nil
}

func (c *vclusterClient) createCredsSecret(ctx context.Context, name string, ls map[string]string, access, secret string) error {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: ls},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"access_key_id":     access,
			"secret_access_key": secret,
		},
	}
	_, err := c.core.CoreV1().Secrets(c.namespace).Create(ctx, sec, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create creds secret: %w", err)
	}
	return nil
}

func (c *vclusterClient) createHeadlessService(ctx context.Context, name, component string, port int32, ls map[string]string) error {
	svcLabels := cloneLabels(ls)
	svcLabels["component"] = component
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: svcLabels},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  map[string]string{"osb.korifi/cluster": ls["osb.korifi/cluster"], "component": component},
			Ports: []corev1.ServicePort{{
				Name: "rpc", Port: port, TargetPort: intstr.FromInt32(port),
			}},
		},
	}
	if component == "s3g" {
		svc.Spec.ClusterIP = ""
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Ports = []corev1.ServicePort{{
			Name: "https", Port: 9878, TargetPort: intstr.FromInt32(9878),
		}}
	}
	_, err := c.core.CoreV1().Services(c.namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create service %s: %w", name, err)
	}
	return nil
}

func (c *vclusterClient) createOzoneSTS(ctx context.Context, cluster, component string, port int32, args, initArgs []string, extra []corev1.EnvVar, persist bool, ls map[string]string) error {
	name := cluster + "-" + component
	podLabels := cloneLabels(ls)
	podLabels["component"] = component
	fsGroup := int64(1000)
	replicas := int32(1)
	cpu := resource.MustParse("250m")
	mem := resource.MustParse("512Mi")
	container := corev1.Container{
		Name:            component,
		Image:           ozoneImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            args,
		Env:             extra,
		EnvFrom:         []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: cluster + "-config"}}}},
		Ports:           []corev1.ContainerPort{{ContainerPort: port}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/data"},
			{Name: "tls", MountPath: "/etc/ozone-tls", ReadOnly: true},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)}},
			InitialDelaySeconds: 45,
			PeriodSeconds:       10,
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             boolPtr(true),
			RunAsUser:                int64Ptr(1000),
			AllowPrivilegeEscalation: boolPtr(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: podLabels},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"osb.korifi/cluster": cluster, "component": component}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{FSGroup: &fsGroup, RunAsNonRoot: boolPtr(true), RunAsUser: int64Ptr(1000)},
					Containers:      []corev1.Container{container},
					Volumes: []corev1.Volume{{
						Name: "tls",
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: cluster + "-tls"}},
					}},
				},
			},
		},
	}
	if len(initArgs) > 0 {
		init := container
		init.Name = "init"
		init.Args = initArgs
		init.LivenessProbe = nil
		sts.Spec.Template.Spec.InitContainers = []corev1.Container{init}
	}
	if persist {
		size := resource.MustParse("2Gi")
		sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{{
			ObjectMeta: metav1.ObjectMeta{Name: "data", Labels: podLabels},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
			},
		}}
	} else {
		sts.Spec.Template.Spec.Volumes = append(sts.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}
	_, err := c.core.AppsV1().StatefulSets(c.namespace).Create(ctx, sts, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create statefulset %s: %w", name, err)
	}
	return nil
}

func (c *vclusterClient) waitSTS(ctx context.Context, name string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		sts, err := c.core.AppsV1().StatefulSets(c.namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && sts.Status.ReadyReplicas >= 1 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for StatefulSet %s", name)
}

func (c *vclusterClient) deprovisionLabeled(ctx context.Context, instanceID string) error {
	sel := labels.Set{osbInstanceLabel: instanceID}.AsSelector().String()
	listOpts := metav1.ListOptions{LabelSelector: sel}
	_ = c.core.AppsV1().StatefulSets(c.namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, listOpts)
	svcs, err := c.core.CoreV1().Services(c.namespace).List(ctx, listOpts)
	if err == nil {
		for _, svc := range svcs.Items {
			_ = c.core.CoreV1().Services(c.namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
		}
	}
	_ = c.core.CoreV1().ConfigMaps(c.namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, listOpts)
	_ = c.core.CoreV1().Secrets(c.namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, listOpts)
	_ = c.core.CoreV1().PersistentVolumeClaims(c.namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, listOpts)
	return nil
}

func cloneLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func int64Ptr(v int64) *int64 { return &v }
