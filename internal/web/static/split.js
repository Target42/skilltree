(() => {
  const root = document.getElementById("workspace");
  if (!root) return;

  const KEY = "skilltree.split.v1";
  const defaults = { groups: 240, skills: 220 };
  const min = { groups: 160, skills: 160, detail: 280 };

  function load() {
    try {
      return { ...defaults, ...JSON.parse(localStorage.getItem(KEY) || "{}") };
    } catch {
      return { ...defaults };
    }
  }

  function save(state) {
    localStorage.setItem(KEY, JSON.stringify(state));
  }

  function apply(state) {
    const groups = root.querySelector('[data-pane="groups"]');
    const skills = root.querySelector('[data-pane="skills"]');
    if (groups) groups.style.flex = `0 0 ${state.groups}px`;
    if (skills) skills.style.flex = `0 0 ${state.skills}px`;
  }

  const state = load();
  apply(state);

  root.querySelectorAll(".splitter-v").forEach((splitter) => {
    splitter.addEventListener("pointerdown", (ev) => {
      ev.preventDefault();
      const prevName = splitter.dataset.prev;
      const nextName = splitter.dataset.next;
      const prev = root.querySelector(`[data-pane="${prevName}"]`);
      const next = root.querySelector(`[data-pane="${nextName}"]`);
      if (!prev || !next) return;

      splitter.setPointerCapture(ev.pointerId);
      const startX = ev.clientX;
      const startPrev = prev.getBoundingClientRect().width;
      const startNext = next.getBoundingClientRect().width;
      const total = startPrev + startNext;

      const onMove = (e) => {
        const dx = e.clientX - startX;
        let newPrev = Math.round(startPrev + dx);
        const minPrev = min[prevName] || 140;
        const minNext = min[nextName] || 200;
        newPrev = Math.max(minPrev, Math.min(total - minNext, newPrev));
        const newNext = total - newPrev;
        if (prevName === "groups" || prevName === "skills") {
          state[prevName] = newPrev;
        }
        if (nextName === "skills") {
          state.skills = newNext;
        }
        // detail is flex:1 — only set fixed panes
        apply(state);
      };

      const onUp = () => {
        splitter.releasePointerCapture(ev.pointerId);
        splitter.removeEventListener("pointermove", onMove);
        splitter.removeEventListener("pointerup", onUp);
        save(state);
      };

      splitter.addEventListener("pointermove", onMove);
      splitter.addEventListener("pointerup", onUp);
    });
  });
})();
