// 原生 ES Module 交互脚本 - 无 npm 构建依赖，支持双向无缝连续滚动
import * as wails from "/wails/runtime.js";

// 启动 WML 标记支持与全局挂载
try {
  if (wails?.WML?.Enable) {
    wails.WML.Enable();
  }
} catch (e) {
  console.warn("WML Enable error:", e);
}

// 暴露全局 wails 对象供调用
window.wails = wails;

// Wails v3 权威 Binding ID 映射表 (精确直连 Go 后端内核)
const WAILS_V3_METHOD_IDS = {
  GetSettings: 1342224581,
  SaveSettings: 2511860532,
  SelectCacheDir: 3386796805,
  SwitchCacheDir: 3670921273,
  GetCacheStats: 3066648129,
  ClearAllCache: 1943297618,
  ListBooks: 4134843646,
  DeleteBook: 217172626,
  ImportLocalFile: 2008686914,
  SaveBook: 1979685772,
  GetLastReadBookID: 1724593172,
  GetProgress: 3554848235,
  SaveProgress: 586790146,
  GetChapterList: 1987540905,
  ReadChapter: 959582645,
  ImportSourcesFromFile: 3397234315,
  ImportSourcesFromURL: 3417151942,
  ImportSourcesFromJSON: 257028379,
  GetSourcesCount: 3879627825,
  SearchBooks: 935633330,
  PickScreenColor: 1427130068,
  WindowMinimize: 1032772880,
  WindowToggleMaximize: 1057027598,
  WindowClose: 2765984564
};

// 挂载真实 Wails v3 运行时桥接
console.log("【划水纪】已挂载 Wails v3 真实原生运行时，直连 Go 后端");
window.go = {
  main: {
    App: new Proxy({}, {
      get(_, prop) {
        if (prop === "WindowMinimize") {
          return () => wails.Window.Minimise();
        }
        if (prop === "WindowToggleMaximize") {
          return () => wails.Window.ToggleMaximise();
        }
        if (prop === "WindowClose") {
          return () => wails.Window.Close();
        }
        const id = WAILS_V3_METHOD_IDS[prop];
        if (id) {
          return (...args) => wails.Call.ByID(id, ...args);
        }
        return (...args) => wails.Call.ByName("huashuireader.App." + String(prop), ...args);
      }
    })
  }
};

let currentBook = null;
let loadedChapterIndices = new Set(); // 已挂载的章节集合
let minLoadedIndex = 0;
let maxLoadedIndex = 0;
let isFetchingChapter = false;
let appSettings = null;
let saveDebounceTimer = null;

// DOM 元素获取
const btnWinMin = document.getElementById("btn-win-min");
const btnWinMax = document.getElementById("btn-win-max");
const btnWinClose = document.getElementById("btn-win-close");

// 侧边栏元素
const btnToggleSidebar = document.getElementById("btn-toggle-sidebar");
const chapterSidebar = document.getElementById("chapter-sidebar");
const sidebarBackdrop = document.getElementById("sidebar-backdrop");
const sidebarBookTitle = document.getElementById("sidebar-book-title");
const sidebarChapCount = document.getElementById("sidebar-chap-count");
const sidebarChaptersList = document.getElementById("sidebar-chapters-list");

const readerBookTitle = document.getElementById("reader-book-title");
const readerChapterTitle = document.getElementById("reader-chapter-title");
const readerScrollContainer = document.getElementById("reader-scroll-container");
const readerChaptersWrapper = document.getElementById("reader-chapters-wrapper");
const bottomSentinel = document.getElementById("bottom-sentinel");

const bookshelfList = document.getElementById("bookshelf-list");
const btnImportLocal = document.getElementById("btn-import-local");

// 初始化入口
window.addEventListener("DOMContentLoaded", async () => {
  setupNavigation();
  setupWindowControls();
  setupSidebar();
  setupShortcuts();
  setupContinuousScroll();
  setupSourceSearch();
  setupSettingsUI();
  setupLocalImport();

  await loadSettings();
  await loadBookshelf();
  await restoreLastRead();
  await refreshSourceStatus();
});

// 1. 窗口控制
function setupWindowControls() {
  btnWinMin?.addEventListener("click", (e) => {
    e.stopPropagation();
    try {
      if (wails?.Window?.Minimise) return wails.Window.Minimise();
    } catch (_) {}
    window.go?.main?.App?.WindowMinimize();
  });
  btnWinMax?.addEventListener("click", (e) => {
    e.stopPropagation();
    try {
      if (wails?.Window?.ToggleMaximise) return wails.Window.ToggleMaximise();
    } catch (_) {}
    window.go?.main?.App?.WindowToggleMaximize();
  });
  btnWinClose?.addEventListener("click", async (e) => {
    e.stopPropagation();
    try {
      await saveCurrentVisibleProgress(true);
    } catch (_) {}
    try {
      if (wails?.Window?.Close) return wails.Window.Close();
    } catch (_) {}
    window.go?.main?.App?.WindowClose();
  });
}

// 侧边栏抽屉控制器
function setupSidebar() {
  btnToggleSidebar?.addEventListener("click", (e) => {
    e.stopPropagation();
    toggleSidebar();
  });

  sidebarBackdrop?.addEventListener("click", () => {
    closeSidebar();
  });
}

function toggleSidebar() {
  if (chapterSidebar.classList.contains("open")) {
    closeSidebar();
  } else {
    openSidebar();
  }
}

async function openSidebar() {
  if (!currentBook) {
    return showToast("请先在书架中选择一本小说阅读");
  }

  chapterSidebar.classList.add("open");
  sidebarBackdrop.classList.add("open");
  btnToggleSidebar.classList.add("active");

  sidebarBookTitle.textContent = currentBook.title;
  await loadAndRenderSidebarChapters();
}

function closeSidebar() {
  chapterSidebar.classList.remove("open");
  sidebarBackdrop.classList.remove("open");
  btnToggleSidebar.classList.remove("active");
}

async function loadAndRenderSidebarChapters() {
  if (!currentBook) return;

  sidebarChaptersList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 40px; font-size: 11px;">正在载入目录...</div>`;

  try {
    const chapters = await window.go.main.App.GetChapterList(currentBook.id);
    if (!chapters || chapters.length === 0) {
      sidebarChaptersList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 40px; font-size: 11px;">未能获取到章节目录</div>`;
      sidebarChapCount.textContent = "0 章";
      return;
    }

    sidebarChapCount.textContent = `共 ${chapters.length} 章`;
    const currentActiveIdx = minLoadedIndex;

    sidebarChaptersList.innerHTML = chapters
      .map((c) => {
        const isActive = c.index === currentActiveIdx;
        return `
          <div class="sidebar-chap-item ${isActive ? "active" : ""}" data-index="${c.index}" title="${escapeHTML(c.title)}">
            ${escapeHTML(c.title)}
          </div>
        `;
      })
      .join("");

    sidebarChaptersList.querySelectorAll(".sidebar-chap-item").forEach((item) => {
      item.addEventListener("click", async () => {
        const targetIdx = parseInt(item.dataset.index, 10);
        closeSidebar();
        await openBook(currentBook, targetIdx, 0);
      });
    });

    const activeItem = sidebarChaptersList.querySelector(".sidebar-chap-item.active");
    if (activeItem) {
      activeItem.scrollIntoView({ block: "center", behavior: "auto" });
    }
  } catch (err) {
    sidebarChaptersList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 40px; font-size: 11px; padding: 0 10px;">目录加载异常: ${escapeHTML(err.toString())}</div>`;
  }
}

// 2. 快捷键
function setupShortcuts() {
  window.addEventListener("keydown", async (e) => {
    if (e.ctrlKey && e.altKey && (e.key === "h" || e.key === "H")) {
      e.preventDefault();
      if (window.go?.main?.App) {
        await saveCurrentVisibleProgress(true);
        window.go.main.App.WindowMinimize();
      }
    }
  });
}

// 3. 视图切换
function setupNavigation() {
  const tabs = document.querySelectorAll(".nav-tab");
  const panels = document.querySelectorAll(".view-panel");

  tabs.forEach((tab) => {
    tab.addEventListener("click", async () => {
      const targetId = tab.dataset.target;
      tabs.forEach((t) => t.classList.remove("active"));
      panels.forEach((p) => p.classList.remove("active"));

      tab.classList.add("active");
      const targetPanel = document.getElementById(targetId);
      if (targetPanel) targetPanel.classList.add("active");

      if (targetId === "view-bookshelf") {
        await loadBookshelf();
      } else if (targetId === "view-source") {
        await refreshSourceStatus();
      } else if (targetId === "view-settings") {
        await refreshCacheStatsUI();
      }
    });
  });
}

// 4. 设置与外观
async function loadSettings() {
  if (!window.go?.main?.App) return;
  try {
    appSettings = await window.go.main.App.GetSettings();
    applySettingsToDOM(appSettings);
  } catch (err) {
    console.error("加载设置异常:", err);
  }
}

function setupSettingsUI() {
  const bgInput = document.getElementById("setting-bg-color");
  const bgPicker = document.getElementById("setting-bg-picker");
  const panelInput = document.getElementById("setting-panel-color");
  const panelPicker = document.getElementById("setting-panel-picker");
  const textInput = document.getElementById("setting-text-color");
  const textPicker = document.getElementById("setting-text-picker");
  const textDimInput = document.getElementById("setting-text-dim-color");
  const textDimPicker = document.getElementById("setting-text-dim-picker");
  const accentInput = document.getElementById("setting-accent-color");
  const accentPicker = document.getElementById("setting-accent-picker");

  const fontSelect = document.getElementById("setting-font-family-select");
  const fontInput = document.getElementById("setting-font-family");
  const fontSlider = document.getElementById("setting-font-size");
  const fontVal = document.getElementById("setting-font-size-val");
  const lineHeightSlider = document.getElementById("setting-line-height");
  const lineHeightVal = document.getElementById("setting-line-height-val");

  const syncColor = (textEl, pickerEl, key) => {
    textEl?.addEventListener("change", async (e) => {
      const val = e.target.value;
      if (pickerEl) pickerEl.value = val;
      appSettings[key] = val;
      applySettingsToDOM(appSettings);
      await window.go.main.App.SaveSettings(appSettings);
    });
    pickerEl?.addEventListener("input", async (e) => {
      const val = e.target.value;
      if (textEl) textEl.value = val;
      appSettings[key] = val;
      applySettingsToDOM(appSettings);
      await window.go.main.App.SaveSettings(appSettings);
    });
  };

  syncColor(bgInput, bgPicker, "backgroundColor");
  syncColor(panelInput, panelPicker, "panelColor");
  syncColor(textInput, textPicker, "textColor");
  syncColor(textDimInput, textDimPicker, "textDimColor");
  syncColor(accentInput, accentPicker, "accentColor");

  // 字体选择联动
  fontSelect?.addEventListener("change", async (e) => {
    const val = e.target.value;
    if (val === "custom") {
      fontInput.style.display = "inline-block";
      fontInput.focus();
    } else {
      fontInput.style.display = "none";
      fontInput.value = val;
      appSettings.fontFamily = val;
      applySettingsToDOM(appSettings);
      await window.go.main.App.SaveSettings(appSettings);
    }
  });

  fontInput?.addEventListener("change", async (e) => {
    const val = e.target.value.trim();
    if (val) {
      appSettings.fontFamily = val;
      applySettingsToDOM(appSettings);
      await window.go.main.App.SaveSettings(appSettings);
    }
  });

  // 字号与行高
  fontSlider?.addEventListener("input", async (e) => {
    const val = parseInt(e.target.value, 10);
    if (fontVal) fontVal.textContent = `${val}px`;
    appSettings.fontSize = val;
    applySettingsToDOM(appSettings);
    await window.go.main.App.SaveSettings(appSettings);
  });

  lineHeightSlider?.addEventListener("input", async (e) => {
    const val = parseFloat(e.target.value);
    if (lineHeightVal) lineHeightVal.textContent = val.toFixed(1);
    appSettings.lineHeight = val;
    applySettingsToDOM(appSettings);
    await window.go.main.App.SaveSettings(appSettings);
  });

  // 吸色
  const pickBtn = document.getElementById("btn-screen-pick");
  pickBtn?.addEventListener("click", async () => {
    pickBtn.disabled = true;
    pickBtn.textContent = "🎯 点击取色(ESC取消)...";
    try {
      const hex = await window.go.main.App.PickScreenColor();
      if (hex) {
        if (bgInput) bgInput.value = hex;
        if (bgPicker) bgPicker.value = hex;
        appSettings.backgroundColor = hex;
        appSettings.titleBarColor = hex;
        applySettingsToDOM(appSettings);
        await window.go.main.App.SaveSettings(appSettings);
      }
    } catch (err) {
      if (!String(err).includes("取消")) {
        showToast("吸色提示: " + err);
      }
    } finally {
      pickBtn.disabled = false;
      pickBtn.textContent = "吸色";
    }
  });

  // 缓存目录选择与清空
  document.getElementById("btn-select-cache-dir")?.addEventListener("click", async () => {
    try {
      const newPath = await window.go.main.App.SelectCacheDir();
      if (newPath) {
        document.getElementById("setting-cache-dir").value = newPath;
        showToast("✓ 缓存目录已切换并同步");
        await refreshCacheStatsUI();
      }
    } catch (err) {
      showToast("切换缓存目录失败: " + err);
    }
  });

  document.getElementById("btn-clear-cache")?.addEventListener("click", async () => {
    try {
      await window.go.main.App.ClearAllCache();
      showToast("✓ 缓存已全部清空");
      await refreshCacheStatsUI();
    } catch (err) {
      showToast("清理缓存失败: " + err);
    }
  });
}

async function refreshCacheStatsUI() {
  if (!window.go?.main?.App) return;
  try {
    const stats = await window.go.main.App.GetCacheStats();
    const display = document.getElementById("cache-size-display");
    if (display && stats) {
      display.textContent = `已缓存: ${stats.humanSize || "0 B"} (${stats.totalFiles || 0} 章节)`;
    }
    const cfg = await window.go.main.App.GetSettings();
    const dirInput = document.getElementById("setting-cache-dir");
    if (dirInput && cfg.cacheDir) {
      dirInput.value = cfg.cacheDir;
    }
  } catch (e) {
    console.error("更新缓存统计异常:", e);
  }
}

function applySettingsToDOM(cfg) {
  if (!cfg) return;
  const root = document.documentElement;
  const bg = cfg.backgroundColor || "#1E1E1E";
  const panel = cfg.panelColor || "#252526";
  const txt = cfg.textColor || "#D4D4D4";
  const textDim = cfg.textDimColor || "#737373";
  const accent = cfg.accentColor || "#8E8E8E";
  const fontFam = cfg.fontFamily || "Microsoft YaHei";
  const fontSize = cfg.fontSize || 14;
  const lineHeight = cfg.lineHeight || 1.6;

  root.style.setProperty("--bg", bg);
  root.style.setProperty("--panel", panel);
  root.style.setProperty("--panel-bg", panel);
  root.style.setProperty("--titlebar-bg", bg);
  root.style.setProperty("--text", txt);
  root.style.setProperty("--text-dim", textDim);
  root.style.setProperty("--accent", accent);
  root.style.setProperty("--font-content", `"${fontFam}", -apple-system, sans-serif`);
  root.style.setProperty("--font-size", `${fontSize}px`);
  root.style.setProperty("--line-height", lineHeight);

  const bgInput = document.getElementById("setting-bg-color");
  const bgPicker = document.getElementById("setting-bg-picker");
  const panelInput = document.getElementById("setting-panel-color");
  const panelPicker = document.getElementById("setting-panel-picker");
  const textInput = document.getElementById("setting-text-color");
  const textPicker = document.getElementById("setting-text-picker");
  const textDimInput = document.getElementById("setting-text-dim-color");
  const textDimPicker = document.getElementById("setting-text-dim-picker");
  const accentInput = document.getElementById("setting-accent-color");
  const accentPicker = document.getElementById("setting-accent-picker");

  if (bgInput) bgInput.value = bg;
  if (bgPicker) bgPicker.value = bg;
  if (panelInput) panelInput.value = panel;
  if (panelPicker) panelPicker.value = panel;
  if (textInput) textInput.value = txt;
  if (textPicker) textPicker.value = txt;
  if (textDimInput) textDimInput.value = textDim;
  if (textDimPicker) textDimPicker.value = textDim;
  if (accentInput) accentInput.value = accent;
  if (accentPicker) accentPicker.value = accent;

  const fontSelect = document.getElementById("setting-font-family-select");
  const fontInput = document.getElementById("setting-font-family");
  if (fontSelect && fontInput) {
    const hasOpt = Array.from(fontSelect.options).some((o) => o.value === fontFam);
    if (hasOpt) {
      fontSelect.value = fontFam;
      fontInput.style.display = "none";
    } else {
      fontSelect.value = "custom";
      fontInput.style.display = "inline-block";
      fontInput.value = fontFam;
    }
  }

  const fontSlider = document.getElementById("setting-font-size");
  const fontVal = document.getElementById("setting-font-size-val");
  if (fontSlider) fontSlider.value = fontSize;
  if (fontVal) fontVal.textContent = `${fontSize}px`;

  const lineHeightSlider = document.getElementById("setting-line-height");
  const lineHeightVal = document.getElementById("setting-line-height-val");
  if (lineHeightSlider) lineHeightSlider.value = lineHeight;
  if (lineHeightVal) lineHeightVal.textContent = parseFloat(lineHeight).toFixed(1);
}

// 5. 本地文件导入 (仅挂载一次)
function setupLocalImport() {
  btnImportLocal?.addEventListener("click", async () => {
    try {
      const book = await window.go.main.App.ImportLocalFile();
      if (book) {
        showToast(`✓ 已成功导入《${book.title}》`);
        await loadBookshelf();
        await openBook(book, 0, 0);
      }
    } catch (err) {
      showToast("导入失败: " + err);
    }
  });
}

// 6. 书架管理
async function loadBookshelf() {
  if (!window.go?.main?.App) return;
  try {
    const books = await window.go.main.App.ListBooks();
    renderBookshelf(books || []);
  } catch (err) {
    console.error("加载书架失败:", err);
  }
}

function renderBookshelf(books) {
  if (books.length === 0) {
    bookshelfList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 40px; font-size: 11.5px;">书架空空如也，可导入本地小说或在书源页全网搜索</div>`;
    return;
  }
  bookshelfList.innerHTML = books
    .map(
      (b) => `
    <div class="book-card" data-id="${b.id}" style="display: flex; justify-content: space-between; align-items: center;">
      <div class="book-info-area" style="flex: 1; min-width: 0; padding-right: 8px; cursor: pointer;">
        <div style="font-weight: bold; font-size: 12px; margin-bottom: 3px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">${escapeHTML(b.title)}</div>
        <div style="font-size: 10.5px; color: var(--text-dim);">${b.sourceType === "online" ? "🌐 在线" : "📁 本地"} · ${escapeHTML(b.author || "佚名")} · ${escapeHTML(b.lastChapter || "阅读")}</div>
      </div>
      <div style="display: flex; gap: 6px; align-items: center; flex-shrink: 0;">
        <button class="btn btn-read-book" data-id="${b.id}" style="padding: 3px 8px; font-size: 11px;">阅读</button>
        <button class="btn btn-del-book" data-id="${b.id}" title="从书架删除" style="padding: 3px 6px; font-size: 11px; background: transparent; border-color: var(--border); color: var(--text-dim);">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg>
        </button>
      </div>
    </div>
  `
    )
    .join("");

  bookshelfList.querySelectorAll(".book-card").forEach((card) => {
    const id = card.dataset.id;
    const target = books.find((x) => x.id === id);

    const enterReader = async () => {
      if (target) {
        let startIdx = 0;
        let startOffset = 0;
        try {
          const prog = await window.go.main.App.GetProgress(target.id);
          if (prog) {
            startIdx = prog.chapterIndex || 0;
            startOffset = prog.charOffset || 0;
          }
        } catch (_) {}
        await openBook(target, startIdx, startOffset);
      }
    };

    card.querySelector(".book-info-area")?.addEventListener("click", enterReader);
    card.querySelector(".btn-read-book")?.addEventListener("click", enterReader);

    card.querySelector(".btn-del-book")?.addEventListener("click", async (e) => {
      e.stopPropagation();
      try {
        await window.go.main.App.DeleteBook(id);
        showToast(`✓ 已彻底删除《${target?.title || ""}》`);
        if (currentBook && currentBook.id === id) {
          currentBook = null;
          readerBookTitle.textContent = "尚未加载书籍";
          readerChapterTitle.textContent = "--";
          readerChaptersWrapper.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 40px; font-size: 12px;">当前书籍已从书架移出。</div>`;
        }
        await loadBookshelf();
        await refreshCacheStatsUI();
      } catch (err) {
        showToast("删除异常: " + err);
      }
    });
  });
}

// 7. 连续滚动与阅读器
function setupContinuousScroll() {
  readerScrollContainer.addEventListener("scroll", () => {
    const scrollTop = readerScrollContainer.scrollTop;
    const scrollHeight = readerScrollContainer.scrollHeight;
    const clientHeight = readerScrollContainer.clientHeight;

    // 向上触顶：预载上一章
    if (scrollTop < 80) {
      prependPreviousChapter();
    }
    // 向下触底：预载下一章
    if (scrollTop + clientHeight >= scrollHeight - 120) {
      appendNextChapter();
    }

    // 防抖保存当前进度
    if (saveDebounceTimer) clearTimeout(saveDebounceTimer);
    saveDebounceTimer = setTimeout(() => {
      saveCurrentVisibleProgress();
    }, 400);
  });
}

async function restoreLastRead() {
  if (!window.go?.main?.App) return;
  try {
    const lastID = await window.go.main.App.GetLastReadBookID();
    if (!lastID) return;

    const books = await window.go.main.App.ListBooks();
    const book = books.find((x) => x.id === lastID);
    if (!book) return;

    const prog = await window.go.main.App.GetProgress(lastID);
    const startIdx = prog ? prog.chapterIndex : 0;
    const offset = prog ? prog.charOffset : 0;

    await openBook(book, startIdx, offset);
  } catch (err) {
    console.error("恢复上次阅读记录异常:", err);
  }
}

async function openBook(book, startChapterIdx = 0, scrollOffset = 0) {
  if (!book) return;
  currentBook = book;
  loadedChapterIndices.clear();
  minLoadedIndex = startChapterIdx;
  maxLoadedIndex = startChapterIdx;

  document.querySelectorAll(".nav-tab").forEach((t) => t.classList.remove("active"));
  document.querySelector('.nav-tab[data-target="view-reader"]')?.classList.add("active");
  document.querySelectorAll(".view-panel").forEach((p) => p.classList.remove("active"));
  document.getElementById("view-reader")?.classList.add("active");

  readerBookTitle.textContent = book.title;
  readerChaptersWrapper.innerHTML = `<div id="reader-loading-hint" style="color: var(--text-dim); text-align: center; margin-top: 50px; font-size: 11.5px;">正在加载正文内容，请稍候...</div>`;

  await fetchAndAppendChapter(startChapterIdx, false);
  const loadingHint = document.getElementById("reader-loading-hint");
  if (loadingHint && loadedChapterIndices.size > 0) {
    loadingHint.remove();
  }

  // 恢复滚动位置
  if (scrollOffset > 0) {
    readerScrollContainer.scrollTop = Math.round(scrollOffset);
  } else {
    readerScrollContainer.scrollTop = 0;
  }

  appendNextChapter();
}

async function prependPreviousChapter() {
  if (!currentBook || isFetchingChapter || minLoadedIndex <= 0) return;
  const prevIdx = minLoadedIndex - 1;

  const oldScrollHeight = readerScrollContainer.scrollHeight;
  const oldScrollTop = readerScrollContainer.scrollTop;

  const success = await fetchAndAppendChapter(prevIdx, true);
  if (success) {
    const newScrollHeight = readerScrollContainer.scrollHeight;
    const heightDiff = newScrollHeight - oldScrollHeight;
    readerScrollContainer.scrollTop = oldScrollTop + heightDiff;
  }
}

async function appendNextChapter() {
  if (!currentBook || isFetchingChapter) return;
  const nextIdx = maxLoadedIndex + 1;
  await fetchAndAppendChapter(nextIdx, false);
}

async function fetchAndAppendChapter(index, prepend = false) {
  if (!currentBook || loadedChapterIndices.has(index)) return false;
  isFetchingChapter = true;

  try {
    const data = await window.go.main.App.ReadChapter(currentBook.id, index);
    if (data && data.content) {
      const loadingHint = document.getElementById("reader-loading-hint");
      if (loadingHint) loadingHint.remove();

      loadedChapterIndices.add(index);
      if (index > maxLoadedIndex) maxLoadedIndex = index;
      if (index < minLoadedIndex) minLoadedIndex = index;

      const block = document.createElement("section");
      block.className = "chapter-block";
      block.dataset.index = index;
      block.dataset.title = data.title;

      const titleDiv = document.createElement("div");
      titleDiv.className = "chapter-title-divider";
      titleDiv.textContent = data.title;

      const bodyDiv = document.createElement("div");
      bodyDiv.className = "chapter-body";
      bodyDiv.textContent = data.content;

      block.appendChild(titleDiv);
      block.appendChild(bodyDiv);

      if (prepend) {
        readerChaptersWrapper.prepend(block);
      } else {
        readerChaptersWrapper.appendChild(block);
      }

      if (loadedChapterIndices.size === 1) {
        readerChapterTitle.textContent = data.title;
      }
      return true;
    }
  } catch (err) {
    if (loadedChapterIndices.size === 0) {
      readerChaptersWrapper.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 50px; font-size: 11.5px; padding: 0 16px;">正文加载异常: ${escapeHTML(err.toString())}</div>`;
    }
  } finally {
    isFetchingChapter = false;
  }
  return false;
}

// 续读位置保存：解决 DPI 浮点数以及章内比例
async function saveCurrentVisibleProgress(force = false) {
  if (!currentBook || !window.go?.main?.App) return;

  const blocks = readerChaptersWrapper.querySelectorAll(".chapter-block");
  const containerTop = readerScrollContainer.getBoundingClientRect().top;
  let activeIndex = minLoadedIndex;
  let activeTitle = readerChapterTitle.textContent || "";
  let activeRatio = 0.0;

  for (let i = blocks.length - 1; i >= 0; i--) {
    const block = blocks[i];
    const rect = block.getBoundingClientRect();
    if (rect.top <= containerTop + 60) {
      activeIndex = parseInt(block.dataset.index, 10);
      activeTitle = block.dataset.title;
      const blockHeight = block.offsetHeight || 1;
      const offsetInBlock = Math.max(0, (containerTop - rect.top));
      activeRatio = Math.min(1.0, offsetInBlock / blockHeight);
      break;
    }
  }

  // 滚动时动态更新阅读器顶栏章节标题
  if (activeTitle && readerChapterTitle) {
    readerChapterTitle.textContent = activeTitle;
  }

  // 保证 charOffset 为正整数，彻底避免高 DPI 小数导致 Go 反序列化拒绝
  const cleanOffset = Math.round(readerScrollContainer.scrollTop || 0);

  try {
    await window.go.main.App.SaveProgress({
      bookId: currentBook.id,
      chapterIndex: activeIndex,
      chapterTitle: activeTitle,
      charOffset: cleanOffset,
      ratio: activeRatio,
    });
  } catch (err) {
    console.error("持久化进度异常:", err);
  }
}

// 8. 书源导入与全网聚合搜索
function setupSourceSearch() {
  const searchInput = document.getElementById("source-search-input");
  const btnSearch = document.getElementById("btn-source-search");
  const resultsList = document.getElementById("search-results-list");
  const btnToggleImport = document.getElementById("btn-toggle-import-box");
  const importBox = document.getElementById("source-import-box");
  const urlInput = document.getElementById("source-url-input");
  const btnImportUrl = document.getElementById("btn-import-source-url");
  const jsonInput = document.getElementById("source-json-input");
  const btnImportJson = document.getElementById("btn-import-source-json");
  const btnImportFile = document.getElementById("btn-import-source-file");

  btnToggleImport?.addEventListener("click", () => {
    if (importBox.style.display === "none" || !importBox.style.display) {
      importBox.style.display = "block";
      btnToggleImport.textContent = "收起导入面板";
    } else {
      importBox.style.display = "none";
      btnToggleImport.textContent = "导入书源规则";
    }
  });

  // 1. 本地 JSON 文件导入
  btnImportFile?.addEventListener("click", async () => {
    try {
      const count = await window.go.main.App.ImportSourcesFromFile();
      if (count > 0) {
        showToast(`✓ 成功导入 ${count} 条有效书源规则`);
        importBox.style.display = "none";
        btnToggleImport.textContent = "导入书源规则";
        await refreshSourceStatus();
      }
    } catch (err) {
      showToast("本地导入异常: " + err);
    }
  });

  // 2. 网络链接导入
  btnImportUrl?.addEventListener("click", async () => {
    const url = urlInput.value.trim();
    if (!url) return showToast("请输入有效的书源链接");
    btnImportUrl.disabled = true;
    btnImportUrl.textContent = "下载中...";
    try {
      const count = await window.go.main.App.ImportSourcesFromURL(url);
      showToast(`✓ 成功导入 ${count} 条书源规则`);
      urlInput.value = "";
      importBox.style.display = "none";
      btnToggleImport.textContent = "导入书源规则";
      await refreshSourceStatus();
    } catch (err) {
      showToast("订阅失败: " + err);
    } finally {
      btnImportUrl.disabled = false;
      btnImportUrl.textContent = "下载订阅";
    }
  });

  // 3. 文本粘贴导入
  btnImportJson?.addEventListener("click", async () => {
    const text = jsonInput.value.trim();
    if (!text) return showToast("请先粘贴 JSON 内容");
    try {
      const count = await window.go.main.App.ImportSourcesFromJSON(text);
      showToast(`✓ 成功导入 ${count} 条书源规则`);
      jsonInput.value = "";
      importBox.style.display = "none";
      btnToggleImport.textContent = "导入书源规则";
      await refreshSourceStatus();
    } catch (err) {
      showToast("规则解析失败: " + err);
    }
  });

  // 搜索逻辑
  const doSearch = async () => {
    const keyword = searchInput.value.trim();
    if (!keyword) return showToast("请输入小说名或作者");

    btnSearch.disabled = true;
    btnSearch.innerHTML = `<span>搜索中...</span>`;
    resultsList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 30px; font-size: 11.5px;">正在并发检索所有书源，请稍候...</div>`;

    try {
      const items = await window.go.main.App.SearchBooks(keyword);
      if (!items || items.length === 0) {
        resultsList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 30px; font-size: 11.5px;">未搜索到相关小说，可检查书源有效性或更换关键词</div>`;
        return;
      }

      const shelf = (await window.go.main.App.ListBooks()) || [];
      const shelfUrls = new Set(shelf.map((x) => x.bookUrl));

      resultsList.innerHTML = items
        .map((item) => {
          const isAdded = shelfUrls.has(item.bookUrl);
          const author = item.author || "未知作者";
          const sourceName = item.sourceName || "聚合源";
          return `
            <div class="book-card" style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; padding: 6px 8px; border: 1px solid var(--border); border-radius: 4px;">
              <div style="flex: 1; min-width: 0; padding-right: 8px;">
                <div style="font-weight: bold; font-size: 12px; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                  ${escapeHTML(item.title)}
                </div>
                <div style="font-size: 10.5px; color: var(--text-dim); margin-top: 2px;">
                  ${escapeHTML(author)} · <span style="opacity: 0.85;">[${escapeHTML(sourceName)}]</span>
                </div>
              </div>
              <button class="btn btn-add-shelf ${isAdded ? "added" : ""}" 
                      data-title="${encodeURIComponent(item.title)}"
                      data-url="${encodeURIComponent(item.bookUrl)}"
                      data-author="${encodeURIComponent(author)}"
                      data-source="${encodeURIComponent(sourceName)}"
                      data-source-id="${encodeURIComponent(item.sourceUrl || "")}"
                      style="padding: 3px 8px; font-size: 11px;">
                ${isAdded ? "已添加" : "加书架"}
              </button>
            </div>
          `;
        })
        .join("");

      // 绑定加入书架
      resultsList.querySelectorAll(".btn-add-shelf").forEach((btn) => {
        btn.addEventListener("click", async (e) => {
          e.stopPropagation();
          const title = decodeURIComponent(btn.dataset.title);
          const bookUrl = decodeURIComponent(btn.dataset.url);
          const author = decodeURIComponent(btn.dataset.author);
          const sourceName = decodeURIComponent(btn.dataset.source);
          const sourceId = decodeURIComponent(btn.dataset.sourceId || "");
          const isAdded = btn.classList.contains("added");

          // 基于 sourceId + bookUrl 计算稳定确定性 ID，支持删除后再添加继承历史进度
          const stableId = "online_" + simpleHash(sourceId + "_" + bookUrl);

          if (isAdded) {
            try {
              await window.go.main.App.DeleteBook(stableId);
              btn.classList.remove("added");
              btn.textContent = "加书架";
              showToast(`✓ 已从书架移出《${title}》`);
              await loadBookshelf();
            } catch (err) {
              showToast("移出失败: " + err);
            }
          } else {
            try {
              await window.go.main.App.SaveBook({
                id: stableId,
                title: title,
                author: author, // 正确保存真实作者！
                sourceType: "online",
                sourceId: sourceId,
                bookUrl: bookUrl,
                lastChapter: "网络章节",
                totalChaps: 0,
              });
              btn.classList.add("added");
              btn.textContent = "已添加";
              showToast(`✓ 已加入书架《${title}》`);
              await loadBookshelf();
            } catch (err) {
              showToast("加入书架失败: " + err);
            }
          }
        });
      });
    } catch (err) {
      resultsList.innerHTML = `<div style="color: var(--text-dim); text-align: center; margin-top: 30px; font-size: 11.5px;">检索出错: ${escapeHTML(String(err))}</div>`;
    } finally {
      btnSearch.disabled = false;
      btnSearch.innerHTML = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg><span>全网搜索</span>`;
    }
  };

  btnSearch?.addEventListener("click", doSearch);
  searchInput?.addEventListener("keydown", (e) => {
    if (e.key === "Enter") doSearch();
  });
}

async function refreshSourceStatus() {
  if (!window.go?.main?.App) return;
  try {
    const count = await window.go.main.App.GetSourcesCount();
    const el = document.getElementById("source-status");
    if (el) {
      el.textContent = `已就绪 (书源: ${count})`;
    }
  } catch (e) {
    console.error("更新书源状态异常:", e);
  }
}

// 简单字符串哈希
function simpleHash(str) {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = (hash << 5) - hash + str.charCodeAt(i);
    hash |= 0;
  }
  return Math.abs(hash).toString(36);
}

// 优雅 Toast
let toastTimer = null;
function showToast(msg) {
  let toast = document.getElementById("app-toast");
  if (!toast) {
    toast = document.createElement("div");
    toast.id = "app-toast";
    toast.className = "toast-msg";
    document.body.appendChild(toast);
  }
  toast.textContent = msg;
  toast.classList.add("show");

  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    toast.classList.remove("show");
  }, 2200);
}

// 严格 XSS 过滤
function escapeHTML(str) {
  if (!str) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
