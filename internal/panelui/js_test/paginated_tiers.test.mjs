import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const sessionValues = new Map();
globalThis.window = { location: { search: "" } };
globalThis.sessionStorage = {
  getItem(key) {
    return sessionValues.get(key) ?? null;
  },
  setItem(key, value) {
    sessionValues.set(key, String(value));
  },
  removeItem(key) {
    sessionValues.delete(key);
  }
};

async function importStandaloneModule(relativeModulePath) {
  const moduleSource = await readFile(new URL(relativeModulePath, import.meta.url), "utf8");
  const encodedModuleSource = Buffer.from(moduleSource).toString("base64");
  return import(`data:text/javascript;base64,${encodedModuleSource}`);
}

const { fetchAllTiers } = await importStandaloneModule("../static/js/api.js");
const { isCurrentTierAvailable } = await importStandaloneModule("../static/js/components/tier-selection.js");

function createTier(tierNumber) {
  return {
    id: `tier-${tierNumber}`,
    name: `Tier ${tierNumber}`,
    level: tierNumber,
    rpm: tierNumber,
    success_limit: tierNumber * 100
  };
}

function createJSONResponse(responseBody) {
  return new Response(JSON.stringify(responseBody), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

test("fetchAllTiers follows opaque cursors beyond the first 100 tiers", async (testContext) => {
  const originalFetch = globalThis.fetch;
  testContext.after(() => {
    globalThis.fetch = originalFetch;
  });

  const firstPageTiers = Array.from({ length: 100 }, (_, index) => createTier(index + 1));
  const opaqueNextCursor = "opaque/cursor+for=tier-100";
  const requestedCursors = [];

  globalThis.fetch = async (requestURL) => {
    const parsedURL = new URL(String(requestURL), "https://panel.example");
    requestedCursors.push(parsedURL.searchParams.get("cursor") || "");
    assert.equal(parsedURL.searchParams.get("limit"), "100");

    if (requestedCursors.length === 1) {
      return createJSONResponse({
        tiers: firstPageTiers,
        next_cursor: opaqueNextCursor,
        has_more: true,
        total_count: 101
      });
    }
    return createJSONResponse({
      tiers: [createTier(101)],
      next_cursor: "",
      has_more: false,
      total_count: 101
    });
  };

  const response = await fetchAllTiers({ limit: 100 });

  assert.deepEqual(requestedCursors, ["", opaqueNextCursor]);
  assert.equal(response.tiers.length, 101);
  assert.equal(response.tiers[100].id, "tier-101");
  assert.equal(response.has_more, false);
  assert.equal(response.next_cursor, "");
});

test("fetchAllTiers rejects missing and repeated continuation cursors", async (testContext) => {
  const originalFetch = globalThis.fetch;
  testContext.after(() => {
    globalThis.fetch = originalFetch;
  });

  await testContext.test("missing cursor", async () => {
    globalThis.fetch = async () => createJSONResponse({
      tiers: [createTier(1)],
      next_cursor: "",
      has_more: true
    });

    await assert.rejects(
      fetchAllTiers(),
      /缺少后续游标/
    );
  });

  await testContext.test("repeated cursor", async () => {
    globalThis.fetch = async () => createJSONResponse({
      tiers: [createTier(1)],
      next_cursor: "repeated-cursor",
      has_more: true
    });

    await assert.rejects(
      fetchAllTiers({ cursor: "repeated-cursor" }),
      /分页游标重复/
    );
  });
});

test("tier selection recognizes a tier loaded after the first cursor page", () => {
  const tiers = Array.from({ length: 101 }, (_, index) => createTier(index + 1));
  assert.equal(isCurrentTierAvailable(tiers, "tier-101"), true);
});

test("tier selection rejects a stale tier reference", () => {
  const tiers = Array.from({ length: 100 }, (_, index) => createTier(index + 1));
  assert.equal(isCurrentTierAvailable(tiers, "deleted-tier"), false);
});
