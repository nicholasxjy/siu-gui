import { describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Dialogs } from "./Forms";
import { api } from "../api";
import { fixture, result } from "../test/fixture";
import type { Mutate, ScanResult } from "../types";
vi.mock("../api", () => ({
  api: { request: vi.fn(), pickDirectory: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));
const scanned: ScanResult = {
  token: "retained-token",
  source: "/sandbox/My Skills",
  skills: [
    {
      name: "new-skill",
      description: "新技能",
      path: "new-skill",
      installed: false,
    },
    {
      name: "existing-skill",
      description: "原有技能",
      path: "existing-skill",
      installed: true,
    },
  ],
};
const props = () => ({
  snapshot: fixture(),
  mutate: vi.fn<Mutate>().mockResolvedValue(result()),
  busy: "",
  onClose: vi.fn(),
  onDone: vi.fn(),
});
describe("扫描来源的生命周期", () => {
  it("只安装本次扫描中未安装的路径，并在关闭后释放 token", async () => {
    vi.mocked(api.request)
      .mockReset()
      .mockImplementation(
        async <T,>(method: string): Promise<T> =>
          (method === "scan_skills" ? scanned : {}) as T,
      );
    const p = props(),
      view = render(
        <Dialogs
          {...p}
          dialog={{ type: "skill-source", source: "/sandbox/My Skills" }}
        />,
      );
    await userEvent.click(screen.getByRole("button", { name: "查找 Skills" }));
    expect(
      await screen.findByRole("checkbox", { name: /existing-skill/ }),
    ).toBeDisabled();
    await userEvent.click(
      screen.getByRole("button", { name: "安装 1 个 Skill" }),
    );
    expect(p.mutate).toHaveBeenCalledWith(
      "install_skills",
      expect.objectContaining({
        token: "retained-token",
        paths: ["new-skill"],
      }),
      "安装所选 Skills",
    );
    view.unmount();
    await waitFor(() =>
      expect(api.request).toHaveBeenCalledWith("release_scan", {
        token: "retained-token",
      }),
    );
  });
  it("取消仍在下载的扫描后，迟到的结果也会释放", async () => {
    let finish: (scan: ScanResult) => void = () => {};
    vi.mocked(api.request)
      .mockReset()
      .mockImplementation(<T,>(method: string): Promise<T> =>
        method === "scan_skills"
          ? (new Promise<ScanResult>((resolve) => {
              finish = resolve;
            }) as Promise<T>)
          : Promise.resolve({} as T),
      );
    const p = props(),
      view = render(
        <Dialogs
          {...p}
          dialog={{ type: "skill-source", source: "/sandbox/My Skills" }}
        />,
      );
    await userEvent.click(screen.getByRole("button", { name: "查找 Skills" }));
    view.unmount();
    await act(async () => finish(scanned));
    await waitFor(() =>
      expect(api.request).toHaveBeenCalledWith("release_scan", {
        token: "retained-token",
      }),
    );
    expect(p.mutate).not.toHaveBeenCalled();
  });
});
