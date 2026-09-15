import { Download, FileCheck2 } from "lucide-react";
import { useEffect, useState } from "react";

const manifestURL = "/downloads/agent-release.json";

type AgentRelease = {
  version: string;
  architecture: "amd64";
  package_format: "deb";
  download_url: string;
  checksum_url: string;
  sha256: string;
};

type ReleaseState =
  | { status: "loading" | "unavailable"; release?: undefined }
  | { status: "ready"; release: AgentRelease };

function isAgentRelease(value: unknown): value is AgentRelease {
  if (!value || typeof value !== "object") return false;
  const release = value as Partial<AgentRelease>;
  return typeof release.version === "string"
    && release.version.length > 0
    && release.architecture === "amd64"
    && release.package_format === "deb"
    && typeof release.download_url === "string"
    && /^\/downloads\/[A-Za-z0-9.+_~-]+\.deb$/.test(release.download_url)
    && typeof release.checksum_url === "string"
    && /^\/downloads\/[A-Za-z0-9.+_~-]+\.deb\.sha256$/.test(release.checksum_url)
    && typeof release.sha256 === "string"
    && /^[a-f0-9]{64}$/.test(release.sha256);
}

function useAgentRelease(): ReleaseState {
  const [state, setState] = useState<ReleaseState>({ status: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    fetch(manifestURL, {
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal: controller.signal,
    }).then(async (response) => {
      if (!response.ok || !response.headers.get("Content-Type")?.includes("application/json")) {
        throw new Error("release unavailable");
      }
      const value: unknown = await response.json();
      if (!isAgentRelease(value)) throw new Error("invalid release manifest");
      setState({ status: "ready", release: value });
    }).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setState({ status: "unavailable" });
    });
    return () => controller.abort();
  }, []);

  return state;
}

export function AgentDownload({ showUnavailable = false }: { showUnavailable?: boolean }) {
  const state = useAgentRelease();

  if (state.status !== "ready") {
    return showUnavailable && state.status === "unavailable"
      ? <p className="agent-download-unavailable">O instalador ainda não está disponível.</p>
      : null;
  }

  return <div className="agent-download">
    <a className="agent-download-button" download href={state.release.download_url}>
      <Download aria-hidden="true" size={18} />
      Baixar agente (.deb)
    </a>
    <div className="agent-download-details">
      <span>Debian/Ubuntu 64 bits · versão {state.release.version}</span>
      <a href={state.release.checksum_url} rel="noreferrer" target="_blank">
        <FileCheck2 aria-hidden="true" size={14} /> SHA-256
      </a>
    </div>
  </div>;
}
