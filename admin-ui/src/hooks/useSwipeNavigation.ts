import { useCallback, useRef, useState } from "react";

const MOBILE_BREAKPOINT = "(max-width: 960px)";
const ENGAGE_PX = 10;
const EXIT_THRESHOLD = 0.18;
const EXIT_DISTANCE = 0.42;
const SETTLE_MS = 180;
const INTERACTIVE = "button, a, input, select, textarea, [role='slider'], [tabindex], dialog";

export function useSwipeNavigation(canNavigate: (delta: -1 | 1) => boolean, onNavigate: (delta: -1 | 1) => void) {
  const [offset, setOffset] = useState(0);
  const [moving, setMoving] = useState(false);
  const [settling, setSettling] = useState(false);
  const session = useRef<{ x: number; y: number; delta: -1 | 1; engaged: boolean; dx: number } | null>(null);

  const settleBack = useCallback(() => {
    setMoving(false);
    setSettling(true);
    setOffset(0);
    window.setTimeout(() => setSettling(false), SETTLE_MS);
  }, []);

  const onPointerDown = useCallback((event: React.PointerEvent<HTMLElement>) => {
    if (settling || event.pointerType !== "touch" || !window.matchMedia(MOBILE_BREAKPOINT).matches) return;
    const target = event.target as Element | null;
    if (target && target.closest(INTERACTIVE)) return;
    session.current = { x: event.clientX, y: event.clientY, delta: 1, engaged: false, dx: 0 };
    event.currentTarget.setPointerCapture(event.pointerId);
  }, [settling]);

  const onPointerMove = useCallback((event: React.PointerEvent<HTMLElement>) => {
    const current = session.current;
    if (!current) return;
    const dx = event.clientX - current.x;
    const dy = event.clientY - current.y;
    if (!current.engaged) {
      if (Math.abs(dx) < ENGAGE_PX && Math.abs(dy) < ENGAGE_PX) return;
      if (Math.abs(dy) > Math.abs(dx)) {
        session.current = null;
        return;
      }
      current.engaged = true;
      current.delta = dx < 0 ? 1 : -1;
      setMoving(true);
    }
    current.dx = dx;
    setOffset(dx);
  }, []);

  const onPointerUp = useCallback(() => {
    const current = session.current;
    session.current = null;
    if (!current?.engaged) return;
    setMoving(false);
    const width = window.innerWidth;
    if (Math.abs(current.dx) < width * EXIT_THRESHOLD || !canNavigate(current.delta)) {
      settleBack();
      return;
    }
    setSettling(true);
    setOffset(-current.delta * width * EXIT_DISTANCE);
    window.setTimeout(() => {
      onNavigate(current.delta);
      setSettling(false);
      setOffset(0);
    }, SETTLE_MS);
  }, [canNavigate, onNavigate, settleBack]);

  const onPointerCancel = useCallback(() => {
    if (!session.current) return;
    session.current = null;
    settleBack();
  }, [settleBack]);

  const bodyStyle = {
    transform: offset ? `translateX(${offset}px)` : undefined,
    transition: moving ? "none" : "transform .18s cubic-bezier(.22,1,.36,1)",
    willChange: moving ? "transform" as const : undefined,
  };
  const mainStyle = { touchAction: "pan-y" as const };

  return { onPointerDown, onPointerMove, onPointerUp, onPointerCancel, bodyStyle, mainStyle };
}