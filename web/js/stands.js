// ============================================================
// stands.js — управление стендами: быстрый дропдаун в шапке,
// модалка «Управление стендами», CRUD через /api/stands.
// Контракт бэкенда:
//   GET    /api/stands               → {"stands":[{id,name,baseURL,verifyCode,isMock,isActive}]}
//   POST   /api/stands               → {name,baseURL,verifyCode?} → объект стенда | 400
//   PUT    /api/stands/{id}          → обновление (мок-стенду baseURL менять нельзя)
//   DELETE /api/stands/{id}          → 409 с текстом, если активный или мок
//   POST   /api/stands/{id}/activate → {"status":"ok","stand":{...}} — переключает весь движок
// Загружается после ui.js и ДО app.js (глобальное пространство имён).
// ============================================================

// ---------- State ----------

let standsCache = [];

// Секреты verifyCode держим в JS-карте и никогда не кладём в HTML-атрибуты
const standSecrets = new Map();

function getActiveStand() {
  return standsCache.find(s => s && s.isActive) || null;
}

function findStandById(id) {
  return standsCache.find(s => String(s.id) === String(id)) || null;
}

// ---------- Загрузка ----------

async function loadStands() {
  try {
    const res = await fetch('/api/stands');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const data = await res.json();
    standsCache = Array.isArray(data.stands) ? data.stands : [];
  } catch (err) {
    console.error('Failed to load stands:', err);
    const wasOpen = isStandsDropdownOpen();
    standsCache = [];
    rerenderStandsUI();
    if (wasOpen) {
      const box = document.getElementById('standsDropdownList');
      if (box) {
        box.innerHTML = '<div class="px-3 py-4 text-center text-[11px] text-red-400">' +
          '<i class="fa-solid fa-plug-circle-xmark mr-1"></i>Не удалось загрузить стенды: ' +
          escapeHtml(err.message) + '</div>';
      }
    }
    return standsCache;
  }
  rerenderStandsUI();
  // Имя активного стенда в шапке и на Обзоре зависит от кэша
  if (typeof renderHeaderEnv === 'function') renderHeaderEnv();
  return standsCache;
}

function rerenderStandsUI() {
  renderStandsDropdownList();
  if (isStandsModalOpen()) renderStandsCards();
}

// ---------- Дропдаун в шапке ----------

function isStandsDropdownOpen() {
  const dd = document.getElementById('standsDropdown');
  return !!dd && !dd.classList.contains('hidden');
}

function toggleStandsDropdown() {
  const dd = document.getElementById('standsDropdown');
  if (!dd) return;
  if (dd.classList.contains('hidden')) {
    dd.classList.remove('hidden');
    loadStands(); // свежий список при каждом открытии
  } else {
    closeStandsDropdown();
  }
}

function closeStandsDropdown() {
  const dd = document.getElementById('standsDropdown');
  if (dd) dd.classList.add('hidden');
}

// Закрытие дропдауна: клик вне панели
document.addEventListener('click', (e) => {
  if (!isStandsDropdownOpen()) return;
  const wrap = document.getElementById('standsDropdownWrap');
  if (wrap && !wrap.contains(e.target)) closeStandsDropdown();
});

// Закрытие дропдауна: Esc
document.addEventListener('keydown', (e) => {
  if (e.key !== 'Escape') return;
  if (isStandsDropdownOpen()) closeStandsDropdown();
});

function renderStandsDropdownList() {
  const box = document.getElementById('standsDropdownList');
  if (!box) return;

  if (!standsCache.length) {
    box.innerHTML = '<div class="px-3 py-4 text-center text-[11px] text-slate-500 italic">Стенды не найдены</div>';
    return;
  }

  box.innerHTML = standsCache.map(s => {
    const active = !!s.isActive;
    const activeDotCls = s.isMock ? 'bg-amber-400' : 'bg-green-400';
    const mockBadge = s.isMock
      ? '<span class="text-[9px] font-bold px-1.5 py-0.5 rounded border bg-amber-500/10 text-amber-400 border-amber-500/30 shrink-0">МОК</span>'
      : '';
    return `
      <button onclick="activateStand('${escAttr(s.id)}', this)" ${active ? 'disabled' : ''}
        title="${active ? 'Активный стенд' : 'Переключить движок на этот стенд'}"
        class="w-full flex items-start gap-2.5 px-3 py-2.5 text-left transition border-b border-darkborder last:border-b-0 ${active ? 'bg-green-500/10 cursor-default' : 'hover:bg-slate-800/70'}">
        <span class="w-2 h-2 rounded-full mt-1.5 shrink-0 ${active ? activeDotCls + ' animate-pulse' : 'bg-slate-600'}"></span>
        <span class="flex-1 min-w-0">
          <span class="flex items-center gap-1.5 min-w-0">
            <span class="text-xs font-bold truncate ${active ? 'text-green-400' : 'text-white'}">${escapeHtml(s.name)}</span>
            ${mockBadge}
          </span>
          <span class="block font-mono text-[10px] text-slate-500 truncate">${escapeHtml(s.baseURL || '—')}</span>
        </span>
        ${active ? '<i class="fa-solid fa-check text-green-400 text-xs shrink-0 mt-0.5"></i>' : ''}
      </button>`;
  }).join('');
}

async function activateStand(id, btn) {
  const stand = findStandById(id);
  if (!stand || stand.isActive) return;

  await busyWrap(btn, async () => {
    try {
      const res = await fetch('/api/stands/' + encodeURIComponent(id) + '/activate', { method: 'POST' });
      if (!res.ok) throw new Error((await res.text()) || ('HTTP ' + res.status));
      closeStandsDropdown();
      toastSuccess(`Переключено на "${stand.name}"`);
      toastInfo('Токены: загружено хранилище этого стенда');
      if (typeof logTerminal === 'function') {
        logTerminal('INFO', `Активный стенд: ${stand.name} (${stand.baseURL || '—'})`);
      }
      await Promise.all([
        typeof initStatus === 'function' ? initStatus() : Promise.resolve(),
        loadStands(),
        // Хранилище токенов изолировано по стендам — после активации перезагружаем vault
        typeof loadVault === 'function' ? loadVault() : Promise.resolve(),
      ]);
      if (typeof refreshOverview === 'function') refreshOverview();
    } catch (err) {
      toastError('Не удалось переключить стенд: ' + err.message);
    }
  });
}

// ==========================================
// МОДАЛКА «УПРАВЛЕНИЕ СТЕНДАМИ»
// ==========================================

function isStandsModalOpen() {
  const m = document.getElementById('standsModal');
  return !!m && !m.classList.contains('hidden');
}

function openStandsModal() {
  closeStandsDropdown();
  resetStandForm();
  const m = document.getElementById('standsModal');
  if (!m) return;
  m.classList.remove('hidden');
  renderStandsCards();
  loadStands();
}

function closeStandsModal() {
  const m = document.getElementById('standsModal');
  if (m) m.classList.add('hidden');
}

function renderStandsCards() {
  const grid = document.getElementById('standsGrid');
  if (!grid) return;
  standSecrets.clear();

  if (!standsCache.length) {
    grid.innerHTML = '<div class="md:col-span-2 py-6 text-center text-xs text-slate-500 italic border border-dashed border-darkborder rounded-xl">Нет ни одного стенда. Добавьте первый в форме ниже.</div>';
    return;
  }

  grid.innerHTML = standsCache.map(s => {
    const active = !!s.isActive;
    const code = s.verifyCode || '';
    if (code) standSecrets.set(String(s.id), code);

    const badges = [
      active ? '<span class="text-[9px] font-bold px-1.5 py-0.5 rounded border bg-green-500/15 text-green-400 border-green-500/30 shrink-0">АКТИВЕН</span>' : '',
      s.isMock ? '<span class="text-[9px] font-bold px-1.5 py-0.5 rounded border bg-amber-500/10 text-amber-400 border-amber-500/30 shrink-0">МОК</span>' : '',
    ].join('');

    const codeHtml = code
      ? `<span class="inline-flex items-center gap-1.5 bg-slate-950 border border-darkborder rounded px-2 py-0.5">
           <span id="standCodeVal-${escAttr(s.id)}" class="select-none">••••</span>
           <button onclick="toggleStandCode('${escAttr(s.id)}')" title="Показать/скрыть код верификации"
             class="text-slate-500 hover:text-slate-200 transition"><i class="fa-regular fa-eye text-[10px]"></i></button>
         </span>`
      : '<span class="italic">не задан</span>';

    const undeletable = active || s.isMock;
    const deleteTitle = s.isMock ? 'Мок-стенд удалить нельзя' : (active ? 'Активный стенд удалить нельзя' : 'Удалить стенд');

    return `
      <div class="rounded-xl border ${active ? 'border-green-500/60 bg-green-500/5' : 'border-darkborder bg-slate-900/60'} p-3 space-y-2.5 flex flex-col">
        <div class="flex items-center gap-2 min-w-0">
          <span class="w-2 h-2 rounded-full shrink-0 ${active ? (s.isMock ? 'bg-amber-400' : 'bg-green-400') + ' animate-pulse' : 'bg-slate-600'}"></span>
          <span class="text-sm font-bold truncate ${active ? 'text-green-400' : 'text-white'}" title="${escAttr(s.name)}">${escapeHtml(s.name)}</span>
          ${badges}
        </div>
        <div class="font-mono text-[11px] break-all ${active ? 'text-emerald-400' : 'text-slate-400'}" title="Base URL стенда">${escapeHtml(s.baseURL || '—')}</div>
        <div class="text-[10px] text-slate-500 flex items-center gap-1.5 flex-wrap">
          <span class="uppercase tracking-wider font-bold shrink-0">Код верификации:</span> ${codeHtml}
        </div>
        <div class="mt-auto pt-2 border-t border-darkborder flex items-center gap-1.5 flex-wrap">
          <button onclick="activateStand('${escAttr(s.id)}', this)" ${active ? 'disabled' : ''} title="${active ? 'Стенд уже активен' : 'Переключить движок на этот стенд'}"
            class="px-2.5 py-1.5 text-[11px] font-semibold rounded-lg transition flex items-center gap-1.5 ${active ? 'bg-green-500/10 text-green-400 border border-green-500/30 cursor-default disabled:opacity-60' : 'bg-green-600 hover:bg-green-500 text-white shadow-lg shadow-green-600/20'}">
            <i class="fa-solid fa-check"></i><span>Сделать активным</span>
          </button>
          <button onclick="startEditStand('${escAttr(s.id)}')" title="Редактировать стенд"
            class="px-2.5 py-1.5 text-[11px] bg-slate-800 hover:bg-slate-700 text-slate-200 border border-darkborder rounded-lg transition flex items-center gap-1.5">
            <i class="fa-solid fa-pen"></i><span>Ред.</span>
          </button>
          <button onclick="deleteStandClick('${escAttr(s.id)}', this)" ${undeletable ? 'disabled' : ''} title="${deleteTitle}"
            class="ml-auto px-2.5 py-1.5 text-[11px] bg-slate-800 text-red-400 border border-darkborder rounded-lg transition flex items-center gap-1.5 ${undeletable ? 'opacity-40 cursor-not-allowed' : 'hover:bg-red-950'}">
            <i class="fa-solid fa-trash"></i><span>Удалить</span>
          </button>
        </div>
      </div>`;
  }).join('');
}

// Глаз у карточки: показ/скрытие verifyCode из JS-карты секретов
function toggleStandCode(id) {
  const el = document.getElementById('standCodeVal-' + id);
  if (!el) return;
  const shown = el.dataset.shown === '1';
  if (shown) {
    el.textContent = '••••';
    el.dataset.shown = '';
  } else {
    el.textContent = standSecrets.get(String(id)) || '';
    el.dataset.shown = '1';
  }
  const icon = el.parentElement ? el.parentElement.querySelector('button i') : null;
  if (icon) icon.className = shown ? 'fa-regular fa-eye text-[10px]' : 'fa-regular fa-eye-slash text-[10px]';
}

// ---------- Форма добавления/редактирования ----------

function resetStandCodeVisibility() {
  const input = document.getElementById('standVerifyCode');
  const icon = document.getElementById('standVerifyCodeEyeIcon');
  if (input) input.type = 'password';
  if (icon) icon.className = 'fa-regular fa-eye';
}

function resetStandForm() {
  document.getElementById('standEditId').value = '';
  document.getElementById('standFormTitleText').textContent = 'Новый стенд';
  document.getElementById('standName').value = '';
  const urlInput = document.getElementById('standBaseUrl');
  urlInput.value = '';
  urlInput.disabled = false;
  document.getElementById('standBaseUrlHint').classList.add('hidden');
  document.getElementById('standVerifyCode').value = '';
  resetStandCodeVisibility();
  document.getElementById('standSubmitLabel').textContent = 'Добавить стенд';
  document.getElementById('standCancelEditBtn').classList.add('hidden');
}

function cancelStandEdit() {
  resetStandForm();
}

function startEditStand(id) {
  const s = findStandById(id);
  if (!s) return;
  document.getElementById('standEditId').value = s.id;
  document.getElementById('standFormTitleText').textContent = 'Редактирование: ' + s.name;
  document.getElementById('standName').value = s.name || '';

  const urlInput = document.getElementById('standBaseUrl');
  urlInput.value = s.baseURL || '';
  urlInput.disabled = !!s.isMock; // мок-стенду baseURL менять нельзя
  document.getElementById('standBaseUrlHint').classList.toggle('hidden', !s.isMock);

  document.getElementById('standVerifyCode').value = s.verifyCode || '';
  resetStandCodeVisibility();

  document.getElementById('standSubmitLabel').textContent = 'Сохранить изменения';
  document.getElementById('standCancelEditBtn').classList.remove('hidden');

  const form = document.getElementById('standFormBox');
  if (form) {
    form.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    form.classList.add('ring-1', 'ring-fuchsia-500/50');
    setTimeout(() => form.classList.remove('ring-1', 'ring-fuchsia-500/50'), 1600);
  }
}

function toggleStandFormCodeVisibility() {
  const input = document.getElementById('standVerifyCode');
  const icon = document.getElementById('standVerifyCodeEyeIcon');
  if (!input || !icon) return;
  const show = input.type === 'password';
  input.type = show ? 'text' : 'password';
  icon.className = show ? 'fa-regular fa-eye-slash' : 'fa-regular fa-eye';
}

async function submitStandForm(btn) {
  const editId = document.getElementById('standEditId').value.trim();
  const name = document.getElementById('standName').value.trim();
  const baseURL = document.getElementById('standBaseUrl').value.trim();
  const verifyCode = document.getElementById('standVerifyCode').value.trim();

  if (!name) {
    toastError('Укажите название стенда.');
    return;
  }
  if (!editId && !baseURL) {
    toastError('Укажите Base URL стенда.');
    return;
  }

  // Мок-стенду baseURL менять нельзя — не отправляем его вовсе
  const editing = editId ? findStandById(editId) : null;
  const payload = { name };
  if (!(editing && editing.isMock)) payload.baseURL = baseURL;
  if (verifyCode) payload.verifyCode = verifyCode;

  await busyWrap(btn, async () => {
    try {
      const res = await fetch(editId ? '/api/stands/' + encodeURIComponent(editId) : '/api/stands', {
        method: editId ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!res.ok) throw new Error((await res.text()) || ('HTTP ' + res.status));
      toastSuccess(editId ? `Стенд "${name}" обновлён.` : `Стенд "${name}" добавлен.`);
      if (typeof logTerminal === 'function') {
        logTerminal(editId ? 'INFO' : 'SUCCESS', (editId ? 'Обновлён стенд: ' : 'Добавлен стенд: ') + name);
      }
      resetStandForm();
      await loadStands();
      // Активный стенд мог измениться — обновляем шапку и статус движка
      if (typeof initStatus === 'function') initStatus();
    } catch (err) {
      toastError('Ошибка сохранения стенда: ' + err.message);
    }
  });
}

async function deleteStandClick(id, btn) {
  const s = findStandById(id);
  if (!s) return;
  if (s.isActive || s.isMock) {
    toastError(s.isMock ? 'Мок-стенд удалить нельзя.' : 'Активный стенд удалить нельзя — сначала переключитесь на другой.');
    return;
  }

  const ok = await confirmDialog(
    'Удалить стенд?',
    `Стенд «${s.name}» (${s.baseURL || '—'}) будет удалён безвозвратно.`,
    'Удалить'
  );
  if (!ok) return;

  await busyWrap(btn, async () => {
    try {
      const res = await fetch('/api/stands/' + encodeURIComponent(id), { method: 'DELETE' });
      if (!res.ok) throw new Error((await res.text()) || ('HTTP ' + res.status));
      toastSuccess(`Стенд "${s.name}" удалён.`);
      if (typeof logTerminal === 'function') logTerminal('INFO', 'Удалён стенд: ' + s.name);
      // Если удаляемый стенд был открыт в форме редактирования — сбрасываем форму
      const editId = document.getElementById('standEditId').value.trim();
      if (editId && String(editId) === String(id)) resetStandForm();
      await loadStands();
    } catch (err) {
      toastError('Не удалось удалить стенд: ' + err.message);
    }
  });
}
