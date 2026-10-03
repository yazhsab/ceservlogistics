import {
  BrowserMultiFormatReader,
  type IScannerControls,
} from "@zxing/browser";
import { Camera, CameraOff } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button, Dialog, InlineNotice } from "./ui";

export function CameraBarcodeScanner({
  onScan,
  disabled,
}: {
  onScan: (barcode: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);
  const controlsRef = useRef<IScannerControls | undefined>(undefined);
  const lastResult = useRef({ value: "", at: 0 });
  const onScanRef = useRef(onScan);

  useEffect(() => {
    onScanRef.current = onScan;
  }, [onScan]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const start = async () => {
      setError("");
      setReady(false);
      if (!navigator.mediaDevices?.getUserMedia) {
        setError("This browser does not provide camera access. Use the barcode field or a USB/Bluetooth scanner.");
        return;
      }
      try {
        const reader = new BrowserMultiFormatReader();
        const controls = await reader.decodeFromConstraints(
          {
            audio: false,
            video: {
              facingMode: { ideal: "environment" },
              width: { ideal: 1280 },
              height: { ideal: 720 },
            },
          },
          videoRef.current ?? undefined,
          (result) => {
            if (!result || cancelled) return;
            const value = result.getText().trim();
            const now = Date.now();
            if (
              !value ||
              (lastResult.current.value === value &&
                now - lastResult.current.at < 1800)
            )
              return;
            lastResult.current = { value, at: now };
            onScanRef.current(value);
          },
        );
        if (cancelled) controls.stop();
        else {
          controlsRef.current = controls;
          setReady(true);
        }
      } catch (cause) {
        if (cancelled) return;
        const denied =
          cause instanceof DOMException &&
          ["NotAllowedError", "SecurityError"].includes(cause.name);
        setError(
          denied
            ? "Camera access was blocked. Allow camera permission for this site and try again."
            : "The camera could not start. Close other camera apps or use the barcode field.",
        );
      }
    };
    void start();
    return () => {
      cancelled = true;
      controlsRef.current?.stop();
      controlsRef.current = undefined;
      setReady(false);
    };
  }, [open]);

  return (
    <>
      <Button type="button" disabled={disabled} onClick={() => setOpen(true)}>
        <Camera aria-hidden className="h-4 w-4" /> Scan with phone camera
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title="Scan with camera"
        description="Point the rear camera at a CESERV AWB, manual waybill, or individual package barcode."
        footer={<Button onClick={() => setOpen(false)}>Done</Button>}
      >
        <div className="space-y-3">
          <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-slate-950">
            <video
              ref={videoRef}
              className="h-full w-full object-cover"
              muted
              playsInline
              aria-label="Live barcode camera preview"
            />
            <div className="pointer-events-none absolute inset-x-[12%] top-1/2 h-28 -translate-y-1/2 rounded-lg border-2 border-[#d8f25a] shadow-[0_0_0_999px_rgba(2,6,23,.35)]" />
            {!ready && !error ? (
              <p className="absolute inset-x-0 bottom-4 text-center text-sm font-semibold text-white">
                Starting camera…
              </p>
            ) : null}
          </div>
          {error ? (
            <InlineNotice tone="warning" title="Camera unavailable">
              <span className="inline-flex items-start gap-2">
                <CameraOff aria-hidden className="mt-0.5 h-4 w-4 shrink-0" />
                {error}
              </span>
            </InlineNotice>
          ) : (
            <p aria-live="polite" className="text-xs text-slate-500">
              {ready
                ? "Camera is active. Each distinct barcode is submitted once and scan feedback remains visible behind this window."
                : "Your browser will ask for camera permission."}
            </p>
          )}
        </div>
      </Dialog>
    </>
  );
}
