// boot.js — the loading screen.
//
// The canvas is held at opacity 0 until the building is standing (see the boot
// hook at the bottom of peel.js), so without this a cold load is several dark
// seconds of nothing. The loader fills that gap with the building's own title
// treatment over a slim bar, and the bar is driven by the real milestones of
// the boot rather than by a timer: the three floor plans, the graph and the
// equipment, the scene, the shell, the interior, the first frame.
//
// bootProgress(step, label) is called from init() and from the peel boot; it
// only ever counts up. bootFinish() runs from a finally, so a load that fails
// half way still hands the screen over instead of leaving a bar that never
// fills standing on top of the viewer.

const BOOT_TOTAL = 8;
let bootCount = 0;
let bootHandedOver = false;
const bootEl = document.getElementById('boot-loader');
const bootFillEl = document.getElementById('bootFill');
const bootLabelEl = document.getElementById('bootLabel');

function bootProgress(step, label) {
  if (bootHandedOver) return;
  bootCount = Math.min(BOOT_TOTAL, bootCount + (step || 1));
  if (bootFillEl) bootFillEl.style.width = ((bootCount / BOOT_TOTAL) * 100).toFixed(1) + '%';
  if (bootLabelEl && label) bootLabelEl.textContent = label;
}

// One moment, not two: the loader leaves on exactly the curve and the duration
// the canvas fades in on, so the building arrives as the bar goes.
function bootFinish() {
  if (bootHandedOver) return;
  bootHandedOver = true;
  if (bootFillEl) bootFillEl.style.width = '100%';
  document.body.classList.remove('booting');
  if (!bootEl) return;
  bootEl.style.transition = 'opacity 420ms ease';
  bootEl.style.opacity = '0';
  setTimeout(() => { bootEl.style.display = 'none'; }, 460);
}

document.body.classList.add('booting');
// A last resort: nothing is allowed to leave the loader on screen for ever.
setTimeout(bootFinish, 30000);

window.bootProgress = bootProgress;
