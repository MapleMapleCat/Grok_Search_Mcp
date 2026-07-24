export function createCursorPaginationState(pageSize, additionalState = {}) {
  return {
    cursor: "",
    nextCursor: "",
    previousCursors: [],
    hasMore: false,
    pageSize: Number(pageSize),
    ...additionalState
  };
}

export function moveCursorPagination(pagination, direction, markLoading = false) {
  if (!pagination || pagination.loadingRecords) {
    return null;
  }

  const snapshot = snapshotCursorPagination(pagination);
  if (direction === "next" && pagination.hasMore && pagination.nextCursor) {
    pagination.previousCursors.push(pagination.cursor);
    pagination.cursor = pagination.nextCursor;
  } else if (direction === "previous" && pagination.previousCursors.length > 0) {
    pagination.cursor = pagination.previousCursors.pop() || "";
  } else {
    return null;
  }
  if (markLoading) {
    pagination.loadingRecords = true;
  }
  return snapshot;
}

export function resetCursorPagination(pagination, pageSize, markLoading = false) {
  if (!pagination || pagination.loadingRecords || Number(pageSize) === pagination.pageSize) {
    return null;
  }
  const snapshot = snapshotCursorPagination(pagination);
  Object.assign(pagination, createCursorPaginationState(pageSize));
  if (markLoading) {
    pagination.loadingRecords = true;
  }
  return snapshot;
}

export function commitCursorPagination(pagination, response) {
  pagination.nextCursor = String(response?.next_cursor || "");
  pagination.hasMore = Boolean(response?.has_more && pagination.nextCursor);
  if (Object.hasOwn(pagination, "loadingRecords")) {
    pagination.loadingRecords = false;
  }
}

export function restoreCursorPagination(pagination, snapshot) {
  if (!pagination || !snapshot) {
    return;
  }
  Object.assign(pagination, snapshot, { previousCursors: [...snapshot.previousCursors] });
  if (Object.hasOwn(pagination, "loadingRecords")) {
    pagination.loadingRecords = false;
  }
}

function snapshotCursorPagination(pagination) {
  return {
    cursor: pagination.cursor,
    nextCursor: pagination.nextCursor,
    previousCursors: [...pagination.previousCursors],
    hasMore: pagination.hasMore,
    pageSize: pagination.pageSize
  };
}
