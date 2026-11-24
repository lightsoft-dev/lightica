import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";

import { UpdateNotification } from "./update-notification";
import type { UpdaterDownloadState } from "../../../shared/updater-types";
import { LIGHTICA_RELEASES_URL } from "../../../shared/lightica";

function renderBanner() {
  return render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <UpdateNotification />
    </I18nProvider>,
  );
}

const mocks = vi.hoisted(() => ({
  installUpdate: vi.fn(),
  openExternal: vi.fn(),
}));

type UpdateDownloadedListener = (info: {
  version: string;
  releaseNotes?: string;
}) => void;
type DownloadStateListener = (state: UpdaterDownloadState) => void;

describe("UpdateNotification", () => {
  let updateDownloaded: UpdateDownloadedListener;
  let downloadState: DownloadStateListener;
  let initialDownloadState: UpdaterDownloadState;

  beforeEach(() => {
    mocks.installUpdate.mockReset().mockResolvedValue(undefined);
    mocks.openExternal.mockReset().mockResolvedValue(undefined);
    initialDownloadState = { status: "idle" };

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: { openExternal: mocks.openExternal },
    });
    Object.defineProperty(window, "updater", {
      configurable: true,
      value: {
        onUpdateDownloaded: (listener: UpdateDownloadedListener) => {
          updateDownloaded = listener;
          return vi.fn();
        },
        installUpdate: mocks.installUpdate,
        onDownloadState: (listener: DownloadStateListener) => {
          downloadState = listener;
          return vi.fn();
        },
        getDownloadState: () => Promise.resolve(initialDownloadState),
      },
    });
  });

  it("opens the Lightica release page from the update prompt", () => {
    renderBanner();
    act(() => updateDownloaded({ version: "0.11.0-20261119" }));

    expect(screen.queryByRole("button", { name: "Later" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "릴리스 기록" }));

    expect(mocks.openExternal).toHaveBeenCalledWith(LIGHTICA_RELEASES_URL);
  });

  describe("when this app cannot install the update itself", () => {
    const manual: UpdaterDownloadState = {
      status: "manual",
      version: "0.11.0-20261120",
      downloadUrl: "https://feed.example/lightica-0.11.0-20261120-mac-arm64.dmg",
    };

    it("links to the dmg instead of offering a restart", () => {
      renderBanner();
      act(() => downloadState(manual));

      expect(screen.getByText("Lightica v0.11.0-20261120")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "지금 업데이트" })).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "다운로드" }));
      expect(mocks.openExternal).toHaveBeenCalledWith(manual.downloadUrl);
      fireEvent.click(screen.getByRole("button", { name: "릴리스 기록" }));
      expect(mocks.openExternal).toHaveBeenCalledWith(LIGHTICA_RELEASES_URL);
    });

    it("shows a version found before the window opened", async () => {
      initialDownloadState = manual;
      renderBanner();
      expect(await screen.findByText("Lightica v0.11.0-20261120")).toBeInTheDocument();
    });

    it("stays dismissed until a newer version appears", () => {
      renderBanner();
      act(() => downloadState(manual));
      fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

      act(() => downloadState(manual));
      expect(screen.queryByText("Lightica v0.11.0-20261120")).not.toBeInTheDocument();

      act(() => downloadState({ ...manual, version: "0.11.0-20261121" }));
      expect(screen.getByText("Lightica v0.11.0-20261121")).toBeInTheDocument();
    });
  });

  it("offers the update in a window opened after the download", async () => {
    initialDownloadState = { status: "downloaded", version: "0.11.1-20261121" };
    renderBanner();

    expect(await screen.findByText("Lightica v0.11.1-20261121")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "지금 업데이트" }));
    expect(mocks.installUpdate).toHaveBeenCalledOnce();
  });

  it("quits and installs a downloaded update from 지금 업데이트", () => {
    renderBanner();
    act(() => updateDownloaded({ version: "0.11.0-20261121" }));

    expect(screen.getByText("Lightica v0.11.0-20261121")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "지금 업데이트" }));

    expect(mocks.installUpdate).toHaveBeenCalledOnce();
  });
});
