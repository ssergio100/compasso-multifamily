import type { View } from "./types";

export type AppModal = "account" | "bonus" | "device" | "routine" | null;

export interface AppNavigationState {
  compassoNavigation: true;
  view: View;
  selectedId: string;
  modal: AppModal;
  editingRoutineId: string | null;
}

const views = new Set<View>(["now", "limits", "routines", "administration", "communication"]);
const modals = new Set<AppModal>([null, "account", "bonus", "device", "routine"]);

export function initialView(): View {
  const value = new URL(window.location.href).searchParams.get("section") as View | null;
  return value && views.has(value) ? value : "now";
}

export function initialDevice(fallback: string): string {
  return new URL(window.location.href).searchParams.get("device") || fallback;
}

export function isAppNavigationState(value: unknown): value is AppNavigationState {
  if (!value || typeof value !== "object") return false;
  const state = value as Partial<AppNavigationState>;
  return state.compassoNavigation === true
    && typeof state.selectedId === "string"
    && typeof state.view === "string"
    && views.has(state.view as View)
    && modals.has(state.modal as AppModal)
    && (state.editingRoutineId === null || typeof state.editingRoutineId === "string");
}

export function writeNavigationState(state: AppNavigationState, replace = false) {
  const url = new URL(window.location.href);
  if (state.view === "now") url.searchParams.delete("section");
  else url.searchParams.set("section", state.view);
  if (state.selectedId) url.searchParams.set("device", state.selectedId);
  else url.searchParams.delete("device");
  window.history[replace ? "replaceState" : "pushState"](state, "", url);
}
