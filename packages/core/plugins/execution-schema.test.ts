// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { EMPTY_PLUGIN_EXECUTION_STATUS, PluginExecutionStatusSchema } from "../api/schemas";

describe("plugin execution response", () => {
  it("defaults missing flags to disabled and strips secret fields", () => {
    expect(PluginExecutionStatusSchema.parse({ token: "must-not-escape" })).toEqual(EMPTY_PLUGIN_EXECUTION_STATUS);
    expect(PluginExecutionStatusSchema.parse({
      configured: true, enabled: true, run_as: { id: "alice", name: "Alice", token: "secret" }, ciphertext: "secret",
    })).toEqual({ configured: true, enabled: true, run_as: { id: "alice", name: "Alice" } });
  });

  it("fails closed on malformed responses", () => {
    for (const raw of [null, [], { enabled: "true" }, { enabled: true, run_as: { id: 4 } }]) {
      expect(parseWithFallback(raw, PluginExecutionStatusSchema, EMPTY_PLUGIN_EXECUTION_STATUS, {
        endpoint: "GET /api/workspaces/{id}/plugins/execution",
      })).toEqual(EMPTY_PLUGIN_EXECUTION_STATUS);
    }
  });
});
