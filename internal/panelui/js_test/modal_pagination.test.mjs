import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function importStandaloneModule(relativeModulePath) {
  const moduleSource = await readFile(new URL(relativeModulePath, import.meta.url), "utf8");
  const encodedModuleSource = Buffer.from(moduleSource).toString("base64");
  return import(`data:text/javascript;base64,${encodedModuleSource}`);
}

const {
  commitCursorPagination,
  createCursorPaginationState,
  moveCursorPagination,
  resetCursorPagination,
  restoreCursorPagination
} = await importStandaloneModule("../static/js/cursor-pagination.js");

test("modal pagination moves forward and backward with opaque cursors", () => {
  const pagination = {
    ...createCursorPaginationState(20, { loadingRecords: false }),
    nextCursor: "opaque/next+cursor",
    hasMore: true
  };

  const forwardSnapshot = moveCursorPagination(pagination, "next", true);
  assert.ok(forwardSnapshot);
  assert.equal(pagination.cursor, "opaque/next+cursor");
  assert.deepEqual(pagination.previousCursors, [""]);
  assert.equal(pagination.loadingRecords, true);

  commitCursorPagination(pagination, { next_cursor: "final-cursor", has_more: true });
  assert.equal(pagination.loadingRecords, false);
  assert.equal(pagination.nextCursor, "final-cursor");

  const backwardSnapshot = moveCursorPagination(pagination, "previous", true);
  assert.ok(backwardSnapshot);
  assert.equal(pagination.cursor, "");
  assert.deepEqual(pagination.previousCursors, []);
});

test("modal pagination page-size reset can be rolled back after failure", () => {
  const pagination = {
    ...createCursorPaginationState(20, { loadingRecords: false }),
    cursor: "page-two",
    nextCursor: "page-three",
    previousCursors: [""],
    hasMore: true
  };

  const snapshot = resetCursorPagination(pagination, 50, true);
  assert.ok(snapshot);
  assert.deepEqual(pagination, {
    ...createCursorPaginationState(50),
    loadingRecords: true
  });

  restoreCursorPagination(pagination, snapshot);
  assert.equal(pagination.cursor, "page-two");
  assert.equal(pagination.nextCursor, "page-three");
  assert.deepEqual(pagination.previousCursors, [""]);
  assert.equal(pagination.hasMore, true);
  assert.equal(pagination.pageSize, 20);
  assert.equal(pagination.loadingRecords, false);
});

test("modal pagination rejects unavailable moves and unchanged page sizes", () => {
  const pagination = createCursorPaginationState(20);

  assert.equal(moveCursorPagination(pagination, "next"), null);
  assert.equal(moveCursorPagination(pagination, "previous"), null);
  assert.equal(resetCursorPagination(pagination, 20), null);
});

test("collection pagination reuses cursor transitions without modal loading state", () => {
  const pagination = {
    ...createCursorPaginationState(50, { totalCount: 120 }),
    nextCursor: "page-two",
    hasMore: true
  };

  const snapshot = moveCursorPagination(pagination, "next");
  assert.ok(snapshot);
  assert.equal(pagination.cursor, "page-two");
  assert.equal(Object.hasOwn(pagination, "loadingRecords"), false);

  restoreCursorPagination(pagination, snapshot);
  assert.equal(pagination.cursor, "");
  assert.equal(pagination.nextCursor, "page-two");
  assert.equal(pagination.totalCount, 120);
});
