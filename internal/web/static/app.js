// Keystroke-driven capture: Enter commits & advances, "r" reads the device,
// "a" re-applies instrument settings. Device reads fill the mapped inputs, and R
// (if present) is shown live as V/I for immediate bench feedback — the
// authoritative computation is done server-side on commit.
(function () {
  const form = document.getElementById("capform");
  if (!form) return;

  function firstInput() {
    return form.querySelector('input:not([type=checkbox]):not([readonly]), textarea, select');
  }

  async function readDevice() {
    const btn = document.getElementById("read");
    if (btn) { btn.disabled = true; btn.textContent = "Reading…"; }
    try {
      const res = await fetch("/read", { method: "POST" });
      const data = await res.json();
      if (data.error) { flash(data.error, true); return; }
      const r = data.readings || {};
      form.querySelectorAll("input[data-channel]").forEach((inp) => {
        const ch = inp.getAttribute("data-channel");
        if (ch in r) inp.value = r[ch];
      });
      recompute();
      flash("Read OK");
    } catch (e) {
      flash("Read failed: " + e, true);
    } finally {
      if (btn) { btn.disabled = false; btn.textContent = "Read device (r)"; }
    }
  }

  async function apply() {
    try {
      const res = await fetch("/apply", { method: "POST" });
      const data = await res.json();
      flash(data.status || "applied");
    } catch (e) { flash("Apply failed: " + e, true); }
  }

  // Live R = V / I convenience when those specific inputs exist.
  function recompute() {
    const I = form.querySelector("#f_I"), V = form.querySelector("#f_V"), R = form.querySelector("#f_R");
    if (I && V && R) {
      const i = parseFloat(I.value), v = parseFloat(V.value);
      if (isFinite(i) && i !== 0 && isFinite(v)) R.value = (v / i).toPrecision(4);
    }
    updateStatus();
  }
  form.addEventListener("input", recompute);

  // Colour status light: orange powered/ready, green OK, red off-limit, yellow unstable.
  function updateStatus() {
    const el = document.getElementById("status");
    if (!el) return;
    const I = parseFloat((form.querySelector("#f_I") || {}).value);
    const V = parseFloat((form.querySelector("#f_V") || {}).value);
    const R = form.querySelector("#f_R");
    const stableEl = form.querySelector("#f_stable");
    const set = (cls, txt) => { el.className = "statuslight " + cls; el.textContent = "● " + txt; };

    if (!isFinite(I) || !isFinite(V) || I === 0) { set("ready", "Powered · ready"); return; }
    const r = V / I;
    let exp = R ? parseFloat(R.getAttribute("data-expected")) : NaN;
    const limit = isFinite(exp) ? exp * 1.5 : 100;   // acceptance: expected +50%, or 100 mΩ
    const stable = stableEl ? stableEl.checked : true;
    if (r > limit || r > 100) set("fail", "Off limit (" + r.toPrecision(3) + " mΩ)");
    else if (!stable) set("unstable", "Confirm stable");
    else set("pass", "OK (" + r.toPrecision(3) + " mΩ)");
  }
  if (form.querySelector("#f_stable")) form.querySelector("#f_stable").addEventListener("change", updateStatus);

  function flash(msg, bad) {
    let el = document.getElementById("flash");
    if (!el) {
      el = document.createElement("div");
      el.id = "flash";
      el.className = "banner";
      el.style.position = "fixed";
      el.style.bottom = "16px";
      el.style.left = "50%";
      el.style.transform = "translateX(-50%)";
      document.body.appendChild(el);
    }
    el.textContent = msg;
    el.className = "banner" + (bad ? " warn" : "");
    el.style.opacity = "1";
    clearTimeout(el._t);
    el._t = setTimeout(() => { el.style.opacity = "0"; }, 1600);
  }

  document.addEventListener("keydown", (e) => {
    const tag = (e.target.tagName || "").toLowerCase();
    const typing = tag === "textarea" || (tag === "input" && e.target.type === "text");
    if (e.key === "Enter" && !typing && !e.shiftKey) {
      e.preventDefault();
      form.submit();
    } else if ((e.key === "r" || e.key === "R") && !typing) {
      if (document.getElementById("read")) { e.preventDefault(); readDevice(); }
    } else if ((e.key === "a" || e.key === "A") && !typing) {
      if (document.getElementById("apply")) { e.preventDefault(); apply(); }
    }
  });

  const readBtn = document.getElementById("read");
  if (readBtn) readBtn.addEventListener("click", readDevice);
  const applyBtn = document.getElementById("apply");
  if (applyBtn) applyBtn.addEventListener("click", apply);

  const fi = firstInput();
  if (fi) fi.focus();
})();
