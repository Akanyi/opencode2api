// Adapted from RelayFor/web/app-parts/navigation.js for this console shell.
import { t, onChange } from "./i18n.js";

export function initNavigation() {
  const root = document.documentElement;
  const rail = document.querySelector("#navigation-rail");
  const toggle = document.querySelector("#rail-collapse");
  const trigger = document.querySelector("#mobile-menu-trigger");
  const menu = document.querySelector("#mobile-navigation-menu");
  const sources = [...rail.querySelectorAll(".nav-item:not(#rail-collapse)")];
  const storageKey = "opencode2api.sidebar-collapsed";
  const desktop = matchMedia("(min-width: 821px)");
  const hoverMedia = matchMedia("(min-width: 821px) and (any-hover: hover) and (any-pointer: fine)");
  let collapsed = false;
  let preview = false;
  let hoverTimer;
  let open = false;
  try { collapsed = localStorage.getItem(storageKey) === "true"; } catch {}

  const heading = document.createElement("div");
  heading.className = "mobile-nav-menu-title";
  menu.append(heading);
  const entries = sources.map((source) => {
    const option = document.createElement("button");
    option.type = "button";
    option.className = "mobile-nav-option";
    option.setAttribute("role", "menuitem");
    option.append(source.querySelector(".material-symbol").cloneNode(true));
    const label = document.createElement("span");
    option.append(label);
    option.addEventListener("click", () => {
      source.click();
      setOpen(false);
    });
    menu.append(option);
    return { source, option, label };
  });

  function renderCollapse() {
    root.dataset.railCollapsed = String(collapsed);
    const label = preview ? t("固定展开侧边栏", "nav.pin") : collapsed ? t("展开侧边栏", "nav.expand") : t("收起侧边栏", "nav.collapse");
    toggle.title = label;
    toggle.setAttribute("aria-label", label);
    toggle.setAttribute("aria-expanded", String(!collapsed || preview));
    toggle.querySelector(".material-symbol").textContent = collapsed ? "left_panel_open" : "left_panel_close";
    toggle.querySelector(".nav-label").textContent = label;
  }

  function sync() {
    heading.textContent = t("导航", "nav.navigation");
    entries.forEach(({ source, option, label }) => {
      const text = source.querySelector(".nav-label").textContent.trim();
      source.title = text;
      source.setAttribute("aria-label", text);
      label.textContent = text;
      option.classList.toggle("active", source.classList.contains("active"));
      if (source.classList.contains("active")) {
        source.setAttribute("aria-current", "page");
        option.setAttribute("aria-current", "page");
      } else {
        source.removeAttribute("aria-current");
        option.removeAttribute("aria-current");
      }
    });
    trigger.setAttribute("aria-label", open ? t("关闭导航菜单", "nav.closeMenu") : t("打开导航菜单", "nav.openMenu"));
    renderCollapse();
  }

  function setOpen(next) {
    const restoreFocus = !next && menu.contains(document.activeElement);
    open = Boolean(next);
    trigger.setAttribute("aria-expanded", String(open));
    menu.hidden = !open;
    menu.inert = !open;
    menu.setAttribute("aria-hidden", String(!open));
    sync();
    if (open) entries[0]?.option.focus({ preventScroll: true });
    else if (restoreFocus) trigger.focus({ preventScroll: true });
  }

  function cancelHover() { clearTimeout(hoverTimer); }
  function closePreview() {
    cancelHover();
    preview = false;
    delete root.dataset.railPreview;
    renderCollapse();
  }
  rail.addEventListener("pointerenter", (event) => {
    if (event.pointerType === "touch" || event.buttons || !hoverMedia.matches || !collapsed) return;
    cancelHover();
    hoverTimer = setTimeout(() => {
      if (!hoverMedia.matches || !collapsed) return;
      preview = true;
      root.dataset.railPreview = "true";
      renderCollapse();
    }, 450);
  });
  rail.addEventListener("pointerleave", closePreview);
  rail.addEventListener("pointercancel", closePreview);
  rail.addEventListener("pointerdown", cancelHover);
  rail.addEventListener("click", cancelHover);
  toggle.addEventListener("click", () => {
    closePreview();
    collapsed = !collapsed;
    try { localStorage.setItem(storageKey, String(collapsed)); } catch {}
    renderCollapse();
  });
  trigger.addEventListener("click", () => setOpen(!open));
  document.addEventListener("click", (event) => {
    if (open && !menu.contains(event.target) && !trigger.contains(event.target)) setOpen(false);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    closePreview();
    if (open) {
      event.preventDefault();
      setOpen(false);
      trigger.focus({ preventScroll: true });
    }
  });
  menu.addEventListener("keydown", (event) => {
    if (!open || !["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const options = entries.map(({ option }) => option);
    const current = options.indexOf(document.activeElement);
    const next = event.key === "Home" ? 0 : event.key === "End" ? options.length - 1 : current < 0 ? 0 : (current + (event.key === "ArrowDown" ? 1 : -1) + options.length) % options.length;
    options[next].focus();
  });
  window.addEventListener("blur", closePreview);
  hoverMedia.addEventListener("change", closePreview);
  desktop.addEventListener("change", () => { closePreview(); if (open) setOpen(false); });
  onChange(sync);
  sync();
  requestAnimationFrame(() => { root.dataset.railReady = "true"; });
  return { sync, close: () => { closePreview(); setOpen(false); } };
}
