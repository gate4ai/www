// gate4.ai: мелкие улучшения поверх страниц, которые работают и без JS.
(() => {
  "use strict";

  // «Скопировать» — кнопки скрыты, пока JS не загружен.
  for (const btn of document.querySelectorAll("button[data-copy]")) {
    btn.hidden = false;
    const label = btn.textContent;
    btn.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(btn.dataset.copy);
        btn.dataset.state = "done";
        btn.textContent = btn.dataset.done || label;
        setTimeout(() => {
          delete btn.dataset.state;
          btn.textContent = label;
        }, 1600);
      } catch {
        // Буфер обмена недоступен: команда видна и выделяется вручную.
      }
    });
  }

  // Вкладки ОС: без перезагрузки; без JS работают как ссылки ?os=.
  for (const box of document.querySelectorAll("[data-cmd]")) {
    const tabs = box.querySelectorAll("[data-os]");
    for (const tab of tabs) {
      tab.addEventListener("click", (e) => {
        e.preventDefault();
        const os = tab.dataset.os;
        for (const b of document.querySelectorAll("[data-cmd]")) {
          for (const t of b.querySelectorAll("[data-os]")) {
            t.setAttribute("aria-selected", String(t.dataset.os === os));
          }
          for (const p of b.querySelectorAll("[data-os-panel]")) {
            p.hidden = p.dataset.osPanel !== os;
          }
        }
      });
    }
  }

  // Поиск в каталоге: фильтрует карточки на лету; форма работает и без JS.
  const input = document.querySelector("input[data-filter]");
  const cards = document.querySelector("[data-cards]");
  if (input && cards) {
    const empty = document.querySelector("[data-empty]");
    input.addEventListener("input", () => {
      const q = input.value.trim().toLowerCase();
      let shown = 0;
      for (const card of cards.children) {
        const hit = !q || card.textContent.toLowerCase().includes(q);
        card.hidden = !hit;
        if (hit) shown++;
      }
      if (empty) empty.hidden = shown > 0;
    });
  }
})();
