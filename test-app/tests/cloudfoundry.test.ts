import { describe, expect, test } from "bun:test";
import {
  DI_FRAMEWORK_COMMIT,
  EXPECTED_BINDINGS,
  inspectCloudFoundryBindings,
} from "../src/cloudfoundry.ts";

describe("DI Framework Cloud Foundry connector", () => {
  test("injects and classifies Korifi VCAP service bindings", () => {
    const report = inspectCloudFoundryBindings(fixtureEnvironment());

    expect(report.healthy).toBe(true);
    expect(report.commit).toBe(DI_FRAMEWORK_COMMIT);
    expect(report.cloudFoundry).toBe(true);
    expect(report.application?.name).toBe("di-framework-bindings");
    expect(report.application?.space).toBe("space");
    expect(report.services.map((service) => service.name)).toEqual([
      ...EXPECTED_BINDINGS,
    ]);
    expect(report.services.every((service) => service.injected)).toBe(true);
    expect(report.classifications).toEqual({
      postgres: "postgres",
      mysql: "mysql",
      redis: true,
      blobStorage: true,
    });
  });

  test("returns a redacted report", () => {
    const reportJson = JSON.stringify(
      inspectCloudFoundryBindings(fixtureEnvironment()),
    );

    expect(reportJson).not.toContain("test-password");
    expect(reportJson).not.toContain("test-api-key");
    expect(reportJson).toContain("credentialKeys");
  });
});

function fixtureEnvironment(): Record<string, string> {
  const credentials: Record<string, Record<string, unknown>> = {
    aigateway: {
      uri: "https://aigateway.example.test/v1",
      api_key: "test-api-key",
    },
    mongodb: {
      uri: "mongodb://user:test-password@mongodb.example.test/db",
    },
    mysql: {
      uri: "mysql://user:test-password@mysql.example.test/db",
    },
    nats: {
      uri: "tls://user:test-password@nats.example.test:4222",
    },
    opensearch: {
      uri: "https://user:test-password@opensearch.example.test:9200",
    },
    ozone: {
      endpoint: "https://ozone.example.test:9878",
      bucket: "test-bucket",
      access_key_id: "test-access-key",
      secret_access_key: "test-secret-key",
    },
    postgres: {
      uri: "postgres://user:test-password@postgres.example.test/db",
    },
    redis: {
      uri: "rediss://default:test-password@redis.example.test:6379/0",
    },
  };

  return {
    VCAP_APPLICATION: JSON.stringify({
      application_id: "app-guid",
      application_name: "di-framework-bindings",
      application_uris: ["di-framework-bindings.example.test"],
      space_id: "space-guid",
      space_name: "space",
      instance_index: 0,
    }),
    VCAP_SERVICES: JSON.stringify(
      Object.fromEntries(
        EXPECTED_BINDINGS.map((name) => {
          const label = name.replace("-smoke", "");
          return [
            label,
            [
              {
                name,
                label,
                credentials: credentials[label],
              },
            ],
          ];
        }),
      ),
    ),
  };
}
