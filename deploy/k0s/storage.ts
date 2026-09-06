/** Default local-path StorageClass with helper-pod taint tolerations for k0s. */
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";

export class K0sLocalPathStorage extends pulumi.ComponentResource {
	readonly storageClass: k8s.storage.v1.StorageClass;

	constructor(
		name: string,
		args: {
			provider: k8s.Provider;
			dependsOn?: pulumi.Input<pulumi.Resource>[];
		},
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sLocalPathStorage", name, {}, opts);

		const child: pulumi.CustomResourceOptions = {
			parent: this,
			provider: args.provider,
			dependsOn: args.dependsOn,
		};

		const ns = new k8s.core.v1.Namespace(
			`${name}-ns`,
			{ metadata: { name: "local-path-storage" } },
			child,
		);

		const sa = new k8s.core.v1.ServiceAccount(
			`${name}-sa`,
			{ metadata: { name: "local-path-provisioner-service-account", namespace: ns.metadata.name } },
			{ ...child, dependsOn: [ns] },
		);

		const role = new k8s.rbac.v1.ClusterRole(
			`${name}-role`,
			{
				metadata: { name: "local-path-provisioner-role" },
				rules: [
					{
						apiGroups: [""],
						resources: ["nodes", "persistentvolumeclaims", "configmaps", "pods/log"],
						verbs: ["get", "list", "watch"],
					},
					{
						apiGroups: [""],
						resources: ["pods"],
						verbs: ["get", "list", "watch", "create", "patch", "update", "delete"],
					},
					{
						apiGroups: [""],
						resources: ["persistentvolumes"],
						verbs: ["get", "list", "watch", "create", "patch", "update", "delete"],
					},
					{
						apiGroups: [""],
						resources: ["events"],
						verbs: ["create", "patch"],
					},
					{
						apiGroups: ["storage.k8s.io"],
						resources: ["storageclasses"],
						verbs: ["get", "list", "watch"],
					},
				],
			},
			child,
		);

		const binding = new k8s.rbac.v1.ClusterRoleBinding(
			`${name}-binding`,
			{
				metadata: { name: "local-path-provisioner-bind" },
				roleRef: {
					apiGroup: "rbac.authorization.k8s.io",
					kind: "ClusterRole",
					name: role.metadata.name,
				},
				subjects: [
					{
						kind: "ServiceAccount",
						name: sa.metadata.name,
						namespace: ns.metadata.name,
					},
				],
			},
			{ ...child, dependsOn: [sa, role] },
		);

		const config = new k8s.core.v1.ConfigMap(
			`${name}-config`,
			{
				metadata: {
					name: "local-path-config",
					namespace: ns.metadata.name,
				},
				data: {
					"config.json": JSON.stringify({
						nodePathMap: [
							{
								node: "DEFAULT_PATH_FOR_NON_LISTED_NODES",
								paths: ["/opt/local-path-provisioner"],
							},
						],
					}),
					setup: "#!/bin/sh\nwhile getopts \"m:s:p:\" opt; do case $opt in p) absolutePath=$OPTARG ;; s) sizeInBytes=$OPTARG ;; m) volMode=$OPTARG ;; esac; done\nmkdir -m 0777 -p ${absolutePath}\n",
					teardown: "#!/bin/sh\nwhile getopts \"m:s:p:\" opt; do case $opt in p) absolutePath=$OPTARG ;; s) sizeInBytes=$OPTARG ;; m) volMode=$OPTARG ;; esac; done\nrm -rf ${absolutePath}\n",
					"helperPod.yaml": `apiVersion: v1
kind: Pod
metadata:
  name: helper-pod
spec:
  tolerations:
    - operator: Exists
  containers:
    - name: helper-pod
      image: rancher/mirrored-library-busybox:1.36.1
      imagePullPolicy: IfNotPresent
`,
				},
			},
			{ ...child, dependsOn: [ns] },
		);

		const deploy = new k8s.apps.v1.Deployment(
			`${name}-deploy`,
			{
				metadata: {
					name: "local-path-provisioner",
					namespace: ns.metadata.name,
				},
				spec: {
					replicas: 1,
					selector: { matchLabels: { app: "local-path-provisioner" } },
					template: {
						metadata: { labels: { app: "local-path-provisioner" } },
						spec: {
							serviceAccountName: sa.metadata.name,
							tolerations: [{ operator: "Exists" }],
							containers: [
								{
									name: "local-path-provisioner",
									image: "rancher/local-path-provisioner:v0.0.31",
									imagePullPolicy: "IfNotPresent",
									command: [
										"local-path-provisioner",
										"--debug",
										"start",
										"--config",
										"/etc/config/config.json",
									],
									volumeMounts: [
										{
											name: "config-volume",
											mountPath: "/etc/config/",
										},
									],
									env: [
										{
											name: "POD_NAMESPACE",
											valueFrom: {
												fieldRef: { fieldPath: "metadata.namespace" },
											},
										},
										{
											name: "CONFIG_MOUNT_PATH",
											value: "/etc/config/",
										},
									],
								},
							],
							volumes: [
								{
									name: "config-volume",
									configMap: { name: config.metadata.name },
								},
							],
						},
					},
				},
			},
			{ ...child, dependsOn: [ns, sa, binding, config] },
		);

		this.storageClass = new k8s.storage.v1.StorageClass(
			`${name}-sc`,
			{
				metadata: {
					name: "local-path",
					annotations: {
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				provisioner: "rancher.io/local-path",
				volumeBindingMode: "WaitForFirstConsumer",
				reclaimPolicy: "Delete",
			},
			{ ...child, dependsOn: [deploy] },
		);

		this.registerOutputs({
			storageClassName: this.storageClass.metadata.name,
		});
	}
}
