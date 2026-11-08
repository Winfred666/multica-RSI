import { useLayoutEffect, useRef } from "react";

/** Keep the visible thread in place when remote activity reorders the list. */
export function useTimelineScrollAnchor(
  container: HTMLElement | null,
  items: readonly { id: string }[],
  navigationKey: string,
  posting: boolean,
  reveal: (index: number, offset: number) => void,
) {
  const anchor = useRef<{ id: string; top: number } | null>(null);
  const previousIds = useRef<readonly string[]>([]);
  const revealRef = useRef(reveal);
  revealRef.current = reveal;

  useLayoutEffect(() => {
    anchor.current = null;
    if (!container) return;
    const remember = () => {
      const bounds = container.getBoundingClientRect();
      const row = Array.from(container.querySelectorAll<HTMLElement>('[id^="comment-"]')).find((el) => {
        const rect = el.getBoundingClientRect();
        return !el.id.startsWith("comment-body-") && rect.bottom > bounds.top + 16 && rect.top <= bounds.top + 16;
      });
      anchor.current = row ? { id: row.id.slice("comment-".length), top: row.getBoundingClientRect().top - bounds.top } : null;
    };
    remember();
    container.addEventListener("scroll", remember, { passive: true });
    return () => container.removeEventListener("scroll", remember);
  }, [container, navigationKey]);

  useLayoutEffect(() => {
    const ids = items.map((item) => item.id);
    const previous = previousIds.current;
    previousIds.current = ids;
    const saved = anchor.current;
    if (!container || posting || !saved || !previous.length || ids.every((id, index) => id === previous[index])) return;
    const index = ids.indexOf(saved.id);
    if (index < 0) return;
    // Virtuoso may have unmounted the moved row; materialize its new slot first.
    if (previous.indexOf(saved.id) !== index) revealRef.current(index, -saved.top);
    let frame = 0;
    let attempts = 0;
    const align = () => {
      const row = container.querySelector<HTMLElement>(`[id="comment-${saved.id}"]`);
      if (row) container.scrollTop += row.getBoundingClientRect().top - container.getBoundingClientRect().top - saved.top;
      if (++attempts < 8) frame = requestAnimationFrame(align);
    };
    const stop = () => cancelAnimationFrame(frame);
    const inputs = ["wheel", "pointerdown", "touchstart", "keydown"] as const;
    for (const input of inputs) container.addEventListener(input, stop, { passive: true });
    frame = requestAnimationFrame(align);
    return () => {
      stop();
      for (const input of inputs) container.removeEventListener(input, stop);
    };
  }, [items, container, navigationKey, posting]);
}
