import { useCallback, useState } from "react";
import type { PanInfo } from "framer-motion";

const MOBILE_BREAKPOINT = "(max-width: 960px)";
const COMMIT_FRACTION = 0.2;
const COMMIT_VELOCITY = 0.4;

export function useSwipeNavigation(canNavigate: (delta: -1 | 1) => boolean, onNavigate: (delta: -1 | 1) => void) {
  const [enabled] = useState(() => window.matchMedia(MOBILE_BREAKPOINT).matches);

  const handleDragEnd = useCallback((_event: unknown, info: PanInfo) => {
    const width = window.innerWidth;
    const delta: -1 | 1 = info.offset.x < 0 ? 1 : -1;
    if (Math.abs(info.offset.x) < width * COMMIT_FRACTION && Math.abs(info.velocity.x) < COMMIT_VELOCITY) return;
    if (!canNavigate(delta)) return;
    onNavigate(delta);
  }, [canNavigate, onNavigate]);

  return {
    drag: enabled ? ("x" as const) : false,
    dragMomentum: false,
    dragSnapToOrigin: true,
    dragDirectionLock: true,
    whileDrag: { scale: 0.985, opacity: 0.985 },
    onDragEnd: handleDragEnd,
    style: { touchAction: "pan-y" },
  };
}