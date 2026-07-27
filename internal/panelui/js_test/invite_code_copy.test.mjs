import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const sessionValues = new Map();
globalThis.window = { location: { search: "", hash: "" } };
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

async function importBrowserModule(relativeModulePath) {
  const moduleURL = new URL(relativeModulePath, import.meta.url);
  return import(await buildBrowserModuleDataURL(moduleURL));
}

async function buildBrowserModuleDataURL(moduleURL) {
  let moduleSource = await readFile(moduleURL, "utf8");
  const relativeImportPattern = /from\s+["'](\.[^"']+)["']/g;
  const relativeSpecifiers = Array.from(
    moduleSource.matchAll(relativeImportPattern),
    (match) => match[1]
  );
  for (const relativeSpecifier of new Set(relativeSpecifiers)) {
    const dependencyDataURL = await buildBrowserModuleDataURL(new URL(relativeSpecifier, moduleURL));
    moduleSource = moduleSource
      .replaceAll(`"${relativeSpecifier}"`, `"${dependencyDataURL}"`)
      .replaceAll(`'${relativeSpecifier}'`, `'${dependencyDataURL}'`);
  }
  return `data:text/javascript;base64,${Buffer.from(moduleSource).toString("base64")}`;
}

const { revealInviteCode } = await importBrowserModule("../static/js/api.js");
const { createInviteEvents } = await importBrowserModule("../static/js/events/invite-events.js");
const { renderInvitesPage } = await importBrowserModule("../static/js/pages/invite-codes.js");

test("invite reveal API uses the dedicated encoded admin endpoint", async () => {
  let requestedURL = "";
  let requestedOptions = null;
  globalThis.fetch = async (url, options) => {
    requestedURL = String(url);
    requestedOptions = options;
    return new Response(JSON.stringify({ code: "grok_revealed" }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    });
  };

  const response = await revealInviteCode("invite/id");

  assert.equal(requestedURL, "/panel/v1/admin/invite-codes/invite%2Fid/reveal");
  assert.equal(requestedOptions.method, "POST");
  assert.equal(requestedOptions.body, undefined);
  assert.deepEqual(response, { code: "grok_revealed" });
});

test("invite list renders a copy action without embedding raw invite material", () => {
  const rawInviteCode = "grok_must_not_render";
  const markup = renderInvitesPage({
    pageLoading: false,
    data: {
      invites: [{
        id: "invite-one",
        code: rawInviteCode,
        code_prefix: "grok_prefix",
        registration_count: 1,
        registration_limit: 3,
        enabled: true,
        created_at: "2026-07-27T00:00:00Z"
      }]
    },
    pagination: {
      invites: {
        totalCount: 1,
        hasMore: false,
        previousCursors: [],
        pageSize: 50
      }
    }
  });

  assert.match(markup, /data-action="copy-invite"/);
  assert.match(markup, /data-id="invite-one"/);
  assert.match(markup, /aria-label="复制完整邀请码"/);
  assert.doesNotMatch(markup, new RegExp(rawInviteCode));
  assert.match(markup, /grok_prefix/);
});

test("copying an invite reveals on demand without changing application state", async () => {
  let resolveRevealRequest;
  const revealRequest = new Promise((resolve) => {
    resolveRevealRequest = resolve;
  });
  const state = {
    data: {
      invites: [{ id: "invite-one", code_prefix: "grok_prefix" }]
    },
    pagination: {
      invites: { previousCursors: [] }
    }
  };
  const stateSnapshot = structuredClone(state);
  const copiedValues = [];
  const actionElement = { disabled: false };
  const inviteEvents = createInviteEvents({
    state,
    modalController: {},
    renderApplication() {},
    renderModalRegion() {},
    handleSessionError() {
      return false;
    },
    async loadCurrentPage() {
      return true;
    },
    async copyValue(value) {
      copiedValues.push(value);
    },
    revealInviteCodeRequest() {
      return revealRequest;
    }
  });

  const copyOperation = inviteEvents.copyInviteCode("invite-one", actionElement);
  assert.equal(actionElement.disabled, true);

  resolveRevealRequest({ code: "grok_revealed_secret" });
  await copyOperation;

  assert.deepEqual(copiedValues, ["grok_revealed_secret"]);
  assert.deepEqual(state, stateSnapshot);
  assert.equal(actionElement.disabled, false);
});
