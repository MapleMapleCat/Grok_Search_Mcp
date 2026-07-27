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

const { renderTiersPage } = await importBrowserModule("../static/js/pages/tiers.js");
const { createTierEvents } = await importBrowserModule("../static/js/events/tier-events.js");

function createTierState() {
  const defaultTier = {
    id: "default-tier",
    name: "Default",
    rpm: 10,
    success_limit: 100,
    is_default: true,
    user_count: 4
  };
  const assignedTier = {
    id: "assigned-tier",
    name: "Assigned",
    rpm: 20,
    success_limit: 200,
    is_default: false,
    user_count: 3
  };
  return {
    pageLoading: false,
    data: {
      tiers: [defaultTier, assignedTier],
      defaultTier,
      users: [{ id: "cached-user", tier_id: assignedTier.id }]
    },
    pagination: {
      tiers: {
        totalCount: 2,
        assignedUserCount: 7,
        hasMore: false,
        previousCursors: [],
        pageSize: 50
      }
    }
  };
}

test("assigned non-default tiers remain deletable while the default stays protected", () => {
  const markup = renderTiersPage(createTierState());
  const assignedDeleteButton = markup.match(
    /<button class="tier-delete-button"[^>]*data-id="assigned-tier"([^>]*)>/
  );
  const defaultDeleteButton = markup.match(
    /<button class="tier-delete-button"[^>]*data-id="default-tier"([^>]*)>/
  );

  assert.ok(assignedDeleteButton);
  assert.doesNotMatch(assignedDeleteButton[0], /\bdisabled\b/);
  assert.match(assignedDeleteButton[0], /自动迁移到当前默认方案/);
  assert.ok(defaultDeleteButton);
  assert.match(defaultDeleteButton[0], /\bdisabled\b/);
});

test("tier deletion explains migration, updates cached counts, and reloads the page", async () => {
  const state = createTierState();
  let openedConfirmation = null;
  let deletedTierIdentifier = "";
  let reloadOptions = null;
  const tierEvents = createTierEvents({
    state,
    modalController: {
      openModal(modalState) {
        openedConfirmation = modalState;
      }
    },
    renderApplication() {},
    handleSessionError() {
      return false;
    },
    async loadCurrentPage(options) {
      reloadOptions = options;
      return true;
    },
    async deleteTierRequest(tierIdentifier) {
      deletedTierIdentifier = tierIdentifier;
    }
  });

  tierEvents.openDeleteConfirmation("assigned-tier");

  assert.equal(openedConfirmation.confirmAction, "deleteTier");
  assert.equal(openedConfirmation.identifier, "assigned-tier");
  assert.match(openedConfirmation.message, /3 位用户将自动迁移到“Default”/);
  assert.match(openedConfirmation.message, /当月已使用额度不会重置/);

  await tierEvents.deleteConfirmed("assigned-tier");

  assert.equal(deletedTierIdentifier, "assigned-tier");
  assert.deepEqual(state.data.tiers.map((tier) => tier.id), ["default-tier"]);
  assert.equal(state.data.tiers[0].user_count, 7);
  assert.equal(state.data.defaultTier.user_count, 7);
  assert.equal(state.pagination.tiers.totalCount, 1);
  assert.equal(state.pagination.tiers.assignedUserCount, 7);
  assert.equal(state.data.users, null);
  assert.deepEqual(reloadOptions, { refreshing: true });
});
