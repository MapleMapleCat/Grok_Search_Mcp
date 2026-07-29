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

const { renderUsersPage } = await importBrowserModule("../static/js/pages/users.js");
const { createUserEvents } = await importBrowserModule("../static/js/events/user-events.js");

function createUser(identifier, username) {
  return {
    id: identifier,
    username,
    role: "user",
    tier_name: "Default",
    enabled: true,
    success_calls: 1,
    success_limit: 100,
    created_at: "2026-01-01T00:00:00Z"
  };
}

test("user page keeps every row available for in-place search filtering", () => {
  const markup = renderUsersPage({
    pageLoading: false,
    user: null,
    filters: { userSearch: "ALICE" },
    data: {
      users: [
        createUser("user-1", "Alice"),
        createUser("user-2", "Bob")
      ]
    },
    pagination: {
      users: {
        totalCount: 2,
        previousCursors: [],
        hasMore: true,
        nextCursor: "next-page"
      }
    }
  });

  assert.match(markup, /data-user-search-results/);
  assert.match(markup, /data-user-search-username="alice" data-user-search-id="user-1"/);
  assert.match(markup, /data-user-search-username="bob" data-user-search-id="user-2" hidden/);
  assert.match(markup, /data-pagination-current-count>1<\/span>/);
  assert.match(markup, />Bob<\/strong>/);
});

test("search input filters existing rows without rendering the application again", () => {
  const aliceRowElement = {
    dataset: { userSearchUsername: "alice", userSearchId: "user-1" },
    hidden: false
  };
  const bobRowElement = {
    dataset: { userSearchUsername: "bob", userSearchId: "user-2" },
    hidden: false
  };
  const emptyStateElement = { hidden: true };
  const userTableElement = { hidden: false };
  const paginationCountElement = { textContent: "2" };
  const paginationElement = {
    hidden: false,
    querySelector(selector) {
      return selector === "[data-pagination-current-count]" ? paginationCountElement : null;
    }
  };
  const searchResultsElement = {
    querySelectorAll(selector) {
      return selector === "[data-user-search-username]"
        ? [aliceRowElement, bobRowElement]
        : [];
    },
    querySelector(selector) {
      const elementsBySelector = {
        "[data-user-search-empty]": emptyStateElement,
        "[data-user-search-table]": userTableElement,
        "[data-user-search-pagination]": paginationElement
      };
      return elementsBySelector[selector] || null;
    }
  };
  const applicationElement = {
    querySelector(selector) {
      return selector === "[data-user-search-results]" ? searchResultsElement : null;
    }
  };
  const state = {
    filters: { userSearch: "" },
    data: { users: [] }
  };
  let applicationRenderCount = 0;
  const userEvents = createUserEvents({
    state,
    applicationElement,
    modalController: {},
    renderApplication() {
      applicationRenderCount += 1;
    },
    handleSessionError() {
      return false;
    }
  });

  userEvents.updateSearchFilter(" BOB ");

  assert.equal(state.filters.userSearch, " BOB ");
  assert.equal(aliceRowElement.hidden, true);
  assert.equal(bobRowElement.hidden, false);
  assert.equal(emptyStateElement.hidden, true);
  assert.equal(userTableElement.hidden, false);
  assert.equal(paginationElement.hidden, false);
  assert.equal(paginationCountElement.textContent, "1");
  assert.equal(applicationRenderCount, 0);

  userEvents.updateSearchFilter("missing");

  assert.equal(aliceRowElement.hidden, true);
  assert.equal(bobRowElement.hidden, true);
  assert.equal(emptyStateElement.hidden, false);
  assert.equal(userTableElement.hidden, true);
  assert.equal(paginationElement.hidden, true);
  assert.equal(paginationCountElement.textContent, "0");
  assert.equal(applicationRenderCount, 0);
});
