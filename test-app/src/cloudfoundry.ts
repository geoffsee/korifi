import { Container } from "@di-framework/core/container";
import {
  CF_ENVIRONMENT_TOKEN,
  CloudFoundryService,
  type CloudFoundryApplicationInfo,
  CloudFoundryEnvironment,
  type CloudFoundryServiceInfo,
  EnableCloudFoundryConnectors,
  type RelationalServiceInfo,
  type RedisServiceInfo,
  VcapApplication,
} from "di-framework/packages/di-framework-cloudfoundry/src/index.ts";

export const DI_FRAMEWORK_COMMIT =
  "5b0cb7fc28eb93fda5a0f9a30969463e1f40e196";

export const EXPECTED_BINDINGS = [
  "aigateway-smoke",
  "mongodb-smoke",
  "mysql-smoke",
  "nats-smoke",
  "opensearch-smoke",
  "ozone-smoke",
  "postgres-smoke",
  "redis-smoke",
] as const;

type ExpectedBindingName = (typeof EXPECTED_BINDINGS)[number];

export interface BindingSummary {
  name: string;
  label: string;
  tags: readonly string[];
  credentialKeys: string[];
  injected: boolean;
}

export interface BindingReport {
  healthy: boolean;
  commit: string;
  cloudFoundry: boolean;
  application: {
    name: string;
    space: string;
    instanceIndex?: number;
  } | null;
  classifications: {
    postgres: string | null;
    mysql: string | null;
    redis: boolean;
    blobStorage: boolean;
  };
  services: BindingSummary[];
  errors: string[];
}

type BoundServiceMap = Record<ExpectedBindingName, CloudFoundryServiceInfo>;

export function inspectCloudFoundryBindings(
  env: Record<string, string | undefined> = process.env,
): BindingReport {
  const container = new Container();

  @EnableCloudFoundryConnectors({ container, env, localFallback: false })
  class ConnectorConfiguration {}

  void ConnectorConfiguration;
  const cf = container.resolve<CloudFoundryEnvironment>(CF_ENVIRONMENT_TOKEN);

  class BoundServices {
    @VcapApplication({ required: true, container })
    application!: CloudFoundryApplicationInfo;

    @CloudFoundryService("aigateway-smoke", { container })
    aigateway!: CloudFoundryServiceInfo;

    @CloudFoundryService("mongodb-smoke", { container })
    mongodb!: CloudFoundryServiceInfo;

    @CloudFoundryService("mysql-smoke", { container })
    mysql!: CloudFoundryServiceInfo;

    @CloudFoundryService("nats-smoke", { container })
    nats!: CloudFoundryServiceInfo;

    @CloudFoundryService("opensearch-smoke", { container })
    opensearch!: CloudFoundryServiceInfo;

    @CloudFoundryService("ozone-smoke", { container })
    ozone!: CloudFoundryServiceInfo;

    @CloudFoundryService("postgres-smoke", { container })
    postgres!: CloudFoundryServiceInfo;

    @CloudFoundryService("redis-smoke", { container })
    redis!: CloudFoundryServiceInfo;
  }

  const decorated = new BoundServices();
  const errors: string[] = [];
  let application: CloudFoundryApplicationInfo | null = null;

  try {
    application = decorated.application;
  } catch (error) {
    errors.push(errorMessage(error));
  }

  const injected = new Map<ExpectedBindingName, CloudFoundryServiceInfo>();
  const decoratedServices: BoundServiceMap = {
    "aigateway-smoke": readService(() => decorated.aigateway, errors),
    "mongodb-smoke": readService(() => decorated.mongodb, errors),
    "mysql-smoke": readService(() => decorated.mysql, errors),
    "nats-smoke": readService(() => decorated.nats, errors),
    "opensearch-smoke": readService(() => decorated.opensearch, errors),
    "ozone-smoke": readService(() => decorated.ozone, errors),
    "postgres-smoke": readService(() => decorated.postgres, errors),
    "redis-smoke": readService(() => decorated.redis, errors),
  };

  for (const expectedName of EXPECTED_BINDINGS) {
    const service = decoratedServices[expectedName];
    if (service.name === expectedName) {
      injected.set(expectedName, service);
    } else {
      errors.push(`decorator did not inject ${expectedName}`);
    }

    try {
      const resolved = container.resolve<CloudFoundryServiceInfo>(
        `cf:service:${expectedName}`,
      );
      if (resolved.name !== expectedName) {
        errors.push(`container token did not resolve ${expectedName}`);
      }
    } catch (error) {
      errors.push(errorMessage(error));
    }
  }

  const services = cf
    .getServiceInfos()
    .map((service): BindingSummary => ({
      name: service.name,
      label: service.label,
      tags: service.tags,
      credentialKeys: Object.keys(service.credentials).sort(),
      injected: injected.has(service.name as ExpectedBindingName),
    }))
    .sort((left, right) => left.name.localeCompare(right.name));

  const postgres = cf.getRelationalServiceInfo("postgres-smoke");
  const mysql = cf.getRelationalServiceInfo("mysql-smoke");
  const redis = cf.getRedisServiceInfo("redis-smoke");
  const blobStorage = cf.getBlobStorageServiceInfo("ozone-smoke");

  expectClassification(postgres, "postgres", "postgres-smoke", errors);
  expectClassification(mysql, "mysql", "mysql-smoke", errors);
  if (redis?.name !== "redis-smoke") {
    errors.push("redis-smoke was not classified as Redis");
  }
  if (blobStorage?.name !== "ozone-smoke") {
    errors.push("ozone-smoke was not classified as blob storage");
  }

  if (!cf.isCloudFoundry()) {
    errors.push("Cloud Foundry environment was not detected");
  }
  if (!application) {
    errors.push("VCAP_APPLICATION was not injected");
  }
  if (services.length !== EXPECTED_BINDINGS.length) {
    errors.push(
      `expected ${EXPECTED_BINDINGS.length} services, discovered ${services.length}`,
    );
  }

  return {
    healthy: errors.length === 0,
    commit: DI_FRAMEWORK_COMMIT,
    cloudFoundry: cf.isCloudFoundry(),
    application: application
      ? {
          name: application.applicationName,
          space: application.spaceName,
          ...(application.instanceIndex === undefined
            ? {}
            : { instanceIndex: application.instanceIndex }),
        }
      : null,
    classifications: {
      postgres: postgres?.dialect ?? null,
      mysql: mysql?.dialect ?? null,
      redis: redis?.name === "redis-smoke",
      blobStorage: blobStorage?.name === "ozone-smoke",
    },
    services,
    errors,
  };
}

function readService(
  getService: () => CloudFoundryServiceInfo,
  errors: string[],
): CloudFoundryServiceInfo {
  try {
    return getService();
  } catch (error) {
    errors.push(errorMessage(error));
    return {
      id: "missing",
      name: "missing",
      label: "missing",
      tags: [],
      credentials: {},
    };
  }
}

function expectClassification(
  service: RelationalServiceInfo | null,
  dialect: "postgres" | "mysql",
  name: ExpectedBindingName,
  errors: string[],
): void {
  if (service?.name !== name || service.dialect !== dialect) {
    errors.push(`${name} was not classified as ${dialect}`);
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
