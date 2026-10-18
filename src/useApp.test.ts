import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { useApplication } from "./useApp";
import { fixture, result as mutationResult } from "./test/fixture";
import type { MutationResult, Snapshot } from "./types";
vi.mock("./api", () => ({
  api: {
    request: vi.fn(),
    info: vi
      .fn()
      .mockResolvedValue({ os: "darwin", arch: "arm64", version: "0.0.1" }),
    setTheme: vi.fn().mockResolvedValue(undefined),
  },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));
beforeEach(() => {
  vi.mocked(api.request).mockReset();
});
describe("写入与快照的一致性", () => {
  it("写入失败后重新读取实际配置，并保留错误而不提示成功", async () => {
    vi.mocked(api.request).mockImplementation(
      async <T>(method: string): Promise<T> => {
        if (method === "snapshot") return fixture() as T;
        throw new Error("写入失败：只读文件");
      },
    );
    const { result } = renderHook(() => useApplication());
    await waitFor(() => expect(result.current.snapshot).not.toBeNull());
    await act(async () => {
      await expect(
        result.current.mutate(
          "set_agents",
          { kind: "mcp", name: "context7", agents: [] },
          "修改分配",
        ),
      ).rejects.toThrow("只读文件");
    });
    expect(api.request).toHaveBeenCalledTimes(3);
    expect(result.current.snapshot?.mcp[0].agents).toEqual(["claude", "codex"]);
    expect(result.current.alert).toEqual(["写入失败：只读文件"]);
    expect(result.current.toast).toBe("");
    expect(result.current.busy).toBe("");
  });
  it("写入进行中拒绝第二次写入，避免快速点击互相覆盖", async () => {
    let finish: (value: MutationResult) => void = () => {};
    vi.mocked(api.request).mockImplementation(
      <T>(method: string): Promise<T> =>
        method === "snapshot"
          ? Promise.resolve(fixture() as T)
          : (new Promise<MutationResult>((resolve) => {
              finish = resolve;
            }) as Promise<T>),
    );
    const { result } = renderHook(() => useApplication());
    await waitFor(() => expect(result.current.snapshot).not.toBeNull());
    let first: Promise<MutationResult>;
    act(() => {
      first = result.current.mutate("set_agents", {}, "修改分配");
    });
    await expect(
      result.current.mutate("set_enabled", {}, "暂停"),
    ).rejects.toThrow("上一项操作尚未完成");
    expect(api.request).toHaveBeenCalledTimes(2);
    await act(async () => {
      finish(mutationResult());
      await first!;
    });
    expect(result.current.busy).toBe("");
  });
  it("写入后的新状态不会被较早启动的读取覆盖", async () => {
    let readCount = 0,
      finishOld: (value: Snapshot) => void = () => {};
    const newer = fixture();
    newer.mcp[0].enabled = false;
    vi.mocked(api.request).mockImplementation(
      <T>(method: string): Promise<T> => {
        if (method !== "snapshot")
          return Promise.resolve(mutationResult(newer) as T);
        if (++readCount === 1) return Promise.resolve(fixture() as T);
        return new Promise<Snapshot>((resolve) => {
          finishOld = resolve;
        }) as Promise<T>;
      },
    );
    const { result } = renderHook(() => useApplication());
    await waitFor(() => expect(result.current.snapshot).not.toBeNull());
    let old: Promise<void>;
    act(() => {
      old = result.current.refresh();
    });
    await act(async () => {
      await result.current.mutate("set_enabled", {}, "暂停");
    });
    await act(async () => {
      finishOld(fixture());
      await old!;
    });
    expect(result.current.snapshot?.mcp[0].enabled).toBe(false);
  });
  it("部分失败保留实际状态与问题，不提示全部成功", async () => {
    const partial = mutationResult();
    partial.snapshot.mcp[0].states.codex = "error";
    partial.report.issues = ["Codex 配置文件写入未完成"];
    vi.mocked(api.request).mockImplementation(
      async <T>(method: string): Promise<T> =>
        (method === "snapshot" ? fixture() : partial) as T,
    );
    const { result } = renderHook(() => useApplication());
    await waitFor(() => expect(result.current.snapshot).not.toBeNull());
    await act(async () => {
      await result.current.mutate("sync", {}, "同步");
    });
    expect(result.current.alert).toEqual(partial.report.issues);
    expect(result.current.snapshot?.mcp[0].states.codex).toBe("error");
    expect(result.current.toast).toBe("");
  });
});
