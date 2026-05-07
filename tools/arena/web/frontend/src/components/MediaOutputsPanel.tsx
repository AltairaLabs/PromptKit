import type { MediaOutput } from "@/types";

interface MediaOutputsPanelProps {
  outputs?: MediaOutput[];
}

function mediaURL(p: string): string {
  const stripped = p.replace(/^\/?out\//, "");
  return `/api/media/${stripped}`;
}

function mediaIcon(t: string): string {
  if (t === "image") return "🖼️";
  if (t === "audio") return "🔊";
  if (t === "video") return "🎬";
  return "📄";
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

// MediaOutputsPanel surfaces every media artifact emitted during the run —
// images, audio, video — with metadata cards mirroring the HTML report's
// "Media Outputs" section.
export function MediaOutputsPanel({ outputs }: MediaOutputsPanelProps) {
  if (!outputs || outputs.length === 0) return null;
  return (
    <div className="space-y-2">
      <h3 className="text-xs font-semibold text-fg-muted uppercase tracking-wider flex items-center gap-2">
        Media Outputs
        <span className="rounded-full bg-[var(--c-surface-2)] text-fg-muted px-2 py-0.5 text-[10px] font-mono">{outputs.length}</span>
      </h3>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        {outputs.map((o, i) => (
          <MediaCard key={i} output={o} />
        ))}
      </div>
    </div>
  );
}

function MediaCard({ output }: { output: MediaOutput }) {
  const url = output.FilePath ? mediaURL(output.FilePath) : null;
  return (
    <div className="rounded-lg bg-surfaceborder border-mist overflow-hidden flex flex-col">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-mist bg-[var(--c-surface-2)]">
        <span className="text-base leading-none">{mediaIcon(output.Type)}</span>
        <span className="text-xs font-medium text-fg capitalize">{output.Type}</span>
        <span className="ml-auto text-[10px] font-mono text-fg-muted truncate max-w-[140px]" title={output.MIMEType}>
          {output.MIMEType}
        </span>
      </div>
      {url && output.Type === "image" && (
        <img src={url} alt="output" className="w-full max-h-48 object-contain bg-[var(--c-surface-2)]" />
      )}
      {url && output.Type === "audio" && (
        <audio controls preload="metadata" src={url} className="w-full" />
      )}
      {url && output.Type === "video" && (
        <video controls preload="metadata" src={url} className="w-full max-h-48" />
      )}
      <div className="px-3 py-2 text-[11px] text-fg-muted space-y-1">
        <Row label="Turn" value={`${output.MessageIdx}.${output.PartIdx}`} />
        <Row label="Size" value={formatBytes(output.SizeBytes)} />
        {output.Duration != null && <Row label="Duration" value={`${output.Duration}s`} />}
        {output.Width != null && output.Height != null && (
          <Row label="Dimensions" value={`${output.Width} × ${output.Height}`} />
        )}
        {output.FilePath && (
          <div className="font-mono text-[10px] text-fg-muted truncate" title={output.FilePath}>
            {output.FilePath}
          </div>
        )}
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-2">
      <span className="text-fg-muted">{label}</span>
      <span className="font-mono text-fg">{value}</span>
    </div>
  );
}
