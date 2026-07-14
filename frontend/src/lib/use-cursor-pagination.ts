"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { CursorPage, CursorPageResponse } from "@/lib/types";

export function useCursorPagination<T>(
  fetchPage: (cursor?: string) => Promise<CursorPageResponse<T>>,
  initialErrorMessage: string,
  moreErrorMessage = "更多记录加载失败",
) {
  const [items, setItems] = useState<T[]>([]);
  const [page, setPage] = useState<CursorPage>({ has_more: false });
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [loadMoreError, setLoadMoreError] = useState("");
  const requestIDRef = useRef(0);
  const loadingMoreRef = useRef(false);

  const reload = useCallback(async () => {
    const requestID = ++requestIDRef.current;
    loadingMoreRef.current = false;
    setLoading(true);
    setLoadingMore(false);
    setError("");
    setLoadMoreError("");
    try {
      const result = await fetchPage();
      if (requestID !== requestIDRef.current) return;
      setItems(result.data);
      setPage(result.page);
    } catch (err) {
      if (requestID !== requestIDRef.current) return;
      setError(err instanceof Error ? err.message : initialErrorMessage);
    } finally {
      if (requestID === requestIDRef.current) setLoading(false);
    }
  }, [fetchPage, initialErrorMessage]);

  useEffect(() => {
    void reload();
    return () => {
      requestIDRef.current += 1;
    };
  }, [reload]);

  const loadMore = useCallback(async () => {
    const cursor = page.next_cursor;
    if (!page.has_more || !cursor || loadingMoreRef.current) return;

    const requestID = requestIDRef.current;
    loadingMoreRef.current = true;
    setLoadingMore(true);
    setLoadMoreError("");
    try {
      const result = await fetchPage(cursor);
      if (requestID !== requestIDRef.current) return;
      setItems((current) => [...current, ...result.data]);
      setPage(result.page);
    } catch (err) {
      if (requestID !== requestIDRef.current) return;
      setLoadMoreError(err instanceof Error ? err.message : moreErrorMessage);
    } finally {
      if (requestID === requestIDRef.current) {
        loadingMoreRef.current = false;
        setLoadingMore(false);
      }
    }
  }, [fetchPage, moreErrorMessage, page.has_more, page.next_cursor]);

  return {
    items,
    setItems,
    page,
    loading,
    loadingMore,
    error,
    loadMoreError,
    loadMore,
    reload,
  };
}
