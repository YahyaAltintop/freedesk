<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, type Component } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import SceneApprove from '@/components/how-it-works/SceneApprove.vue'
import SceneEnterCode from '@/components/how-it-works/SceneEnterCode.vue'
import SceneRunHost from '@/components/how-it-works/SceneRunHost.vue'
import '@/assets/how-it-works.css'

// The home page's "How it works": three steps, each played as a short scene
// drawn in HTML and CSS rather than a video file (sharp at any size, a few KB,
// and its texts are the real program's). The steps run on their own; the
// arrows, the step bar, a swipe or a click on a peeking card move by hand, and
// the pause button stops everything. Playback starts once the reel is on
// screen and stays off for prefers-reduced-motion, where every scene shows its
// last frame. The mouse resting on the reel does NOT hold it: the reel is
// about as tall as the screen, so a visitor who scrolls down is always on it.

interface Step {
  title: string
  text: string
  scene: Component
  /** How long the step plays. Its scene's beats are fractions of this. */
  ms: number
}

const STEPS: readonly Step[] = [
  {
    title: 'Run freedesk.exe',
    text: 'On the PC to share. One file, no install. It shows a 6-digit code.',
    scene: SceneRunHost,
    ms: 7000,
  },
  {
    title: 'Enter the code',
    text: 'Open this page anywhere. Type the code, hit Connect.',
    scene: SceneEnterCode,
    ms: 7000,
  },
  {
    // Longer, so the consent box can be read before Yes is clicked.
    title: 'Click Yes on the host',
    text: 'A box pops up there. Yes, and the screen is yours.',
    scene: SceneApprove,
    ms: 9000,
  },
]
const LAST = STEPS.length - 1

// What the scenes show: an example code and the site's public address, fixed
// so a local or preview build does not show its own host (localhost:9205).
const DEMO_CODE = '482913'
const site = 'free-desk.web.app'

// A sideways drag this long changes the step; a shorter one springs back.
const SWIPE_PX = 48
// Share of the reel that has to be visible before it plays.
const ON_SCREEN_RATIO = 0.35

const active = ref(0)
// The step whose scene and progress bar run. -1 after the user paused and
// moved on: the step they look at shows its last frame until they press play.
const playing = ref(0)
// Bumped to replay a step's scene and progress bar from the start.
const runs = ref(STEPS.map(() => 0))
const userPaused = ref(false)
const keyboardInside = ref(false)
const onScreen = ref(false)
const pageVisible = ref(document.visibilityState === 'visible')

// Everything stands still: paused, scrolled out of view or in a hidden tab.
const frozen = computed(() => userPaused.value || !onScreen.value || !pageVisible.value)
// The scene plays on, but the next step waits.
const holding = computed(() => keyboardInside.value)

function stepNumber(index: number): string {
  return String(index + 1).padStart(2, '0')
}

function show(index: number): void {
  const next = (index + STEPS.length) % STEPS.length
  active.value = next
  if (userPaused.value) {
    playing.value = -1
    return
  }
  runs.value = runs.value.map((count, i) => (i === next ? count + 1 : count))
  playing.value = next
}

function togglePause(): void {
  userPaused.value = !userPaused.value
  // A step moved to while paused starts from its beginning.
  if (!userPaused.value && playing.value !== active.value) {
    show(active.value)
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
    return
  }
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault()
    show(active.value + (event.key === 'ArrowRight' ? 1 : -1))
  }
}

// Keyboard focus in the carousel holds the next step, so it does not move
// away under someone tabbing through it. A mouse click on a control does not:
// one click would otherwise stop the steps for good.
function onFocusIn(event: FocusEvent): void {
  keyboardInside.value = (event.target as HTMLElement).matches(':focus-visible')
}

function onFocusOut(event: FocusEvent): void {
  const section = event.currentTarget as HTMLElement
  if (!section.contains(event.relatedTarget as Node | null)) {
    keyboardInside.value = false
  }
}


// ---- Swipe: the reel follows the finger (or a mouse drag) and settles ------

interface Gesture {
  id: number
  x: number
  y: number
}

let gesture: Gesture | null = null
const dragging = ref(false)
const dragPx = ref(0)
// Set by a drag, so the click it ends with does not also land on a card.
let swallowClick = false

function onPointerDown(event: PointerEvent): void {
  swallowClick = false
  if (!event.isPrimary || (event.pointerType === 'mouse' && event.button !== 0)) {
    return
  }
  gesture = { id: event.pointerId, x: event.clientX, y: event.clientY }
}

function onPointerMove(event: PointerEvent): void {
  if (!gesture || event.pointerId !== gesture.id) {
    return
  }
  const dx = event.clientX - gesture.x
  if (!dragging.value) {
    // Mostly vertical is a page scroll; touch-action leaves that to the browser.
    if (Math.abs(dx) < 8 || Math.abs(dx) < Math.abs(event.clientY - gesture.y)) {
      return
    }
    dragging.value = true
    // Capture keeps the drag going when the pointer leaves the reel. Without
    // it (the pointer is already gone) the drag still works inside it.
    const viewport = event.currentTarget as HTMLElement
    try {
      viewport.setPointerCapture(event.pointerId)
    } catch {
      // NotFoundError: no active pointer with this id.
    }
  }
  // Past either end the reel only gives a little: there is nothing there.
  const pastEnd = (dx > 0 && active.value === 0) || (dx < 0 && active.value === LAST)
  dragPx.value = pastEnd ? dx / 4 : dx
}

function onPointerUp(event: PointerEvent): void {
  if (!gesture || event.pointerId !== gesture.id) {
    return
  }
  const dx = event.clientX - gesture.x
  const dragged = dragging.value
  endGesture()
  if (!dragged) {
    return
  }
  swallowClick = true
  if (dx <= -SWIPE_PX && active.value < LAST) {
    show(active.value + 1)
  } else if (dx >= SWIPE_PX && active.value > 0) {
    show(active.value - 1)
  }
}

function endGesture(): void {
  gesture = null
  dragging.value = false
  dragPx.value = 0
}

function onClickCapture(event: MouseEvent): void {
  if (swallowClick) {
    swallowClick = false
    event.preventDefault()
    event.stopPropagation()
  }
}

// ---- Play only while it can be seen -----------------------------------------

const reel = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | undefined

function onVisibilityChange(): void {
  pageVisible.value = document.visibilityState === 'visible'
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  if (!reel.value) {
    return
  }
  // A visitor who scrolls down meets step 1 at its beginning, not halfway
  // through step 3.
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        onScreen.value = entry.intersectionRatio >= ON_SCREEN_RATIO
      }
    },
    { threshold: [0, ON_SCREEN_RATIO] },
  )
  observer.observe(reel.value)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <section
    id="how"
    class="fd-how"
    :class="{ 'hs-frozen': frozen, 'is-holding': holding }"
    aria-roledescription="carousel"
    aria-label="How FreeDesk works"
    @keydown="onKeydown"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
  >
    <div class="container">
      <div class="fd-how-head">
        <span class="fd-eyebrow">how it works</span>
        <h2 class="fd-h2">Three steps. <span class="fd-gradient-text">One minute.</span></h2>
        <p class="fd-how-lead">No install, no account. Watch the whole thing.</p>
      </div>

      <div ref="reel" class="fd-how-reel">
        <p class="visually-hidden" :aria-live="userPaused ? 'polite' : 'off'">
          Step {{ active + 1 }} of {{ STEPS.length }}: {{ STEPS[active].title }}
        </p>
        <div
          class="fd-how-viewport"
          :class="{ 'is-dragging': dragging }"
          @pointerdown="onPointerDown"
          @pointermove="onPointerMove"
          @pointerup="onPointerUp"
          @pointercancel="endGesture"
          @click.capture="onClickCapture"
        >
          <div class="fd-how-track" :style="{ '--i': active, '--drag': `${dragPx}px` }">
            <div
              v-for="(step, index) in STEPS"
              :key="step.title"
              class="fd-how-card"
              :class="{ 'is-active': index === active }"
              :style="{ '--how-step': `${step.ms}ms` }"
              role="group"
              aria-roledescription="slide"
              :aria-label="`${index + 1} of ${STEPS.length}`"
            >
              <div class="fd-how-frame">
                <div :key="runs[index]" class="fd-how-slide" :class="{ 'hs-run': index === playing }">
                  <component :is="step.scene" :code="DEMO_CODE" :site="site" />
                </div>
                <span v-if="userPaused && index === active" class="fd-how-badge">
                  <AppIcon name="pause" :size="12" /> paused
                </span>
              </div>
              <div class="fd-how-cap">
                <span class="fd-how-cap-num">{{ stepNumber(index) }}</span>
                <div>
                  <h3 class="fd-how-cap-title">{{ step.title }}</h3>
                  <p class="fd-how-cap-text">{{ step.text }}</p>
                </div>
              </div>
              <!-- The peeking neighbours are a mouse and touch shortcut; the
                   step bar below is the same thing for the keyboard. -->
              <button
                v-if="index !== active"
                class="fd-how-peek"
                type="button"
                tabindex="-1"
                aria-hidden="true"
                @click="show(index)"
              ></button>
            </div>
          </div>
        </div>
        <button class="fd-how-arrow is-prev" type="button" aria-label="Previous step" @click="show(active - 1)">
          <AppIcon name="chevron-left" />
        </button>
        <button class="fd-how-arrow is-next" type="button" aria-label="Next step" @click="show(active + 1)">
          <AppIcon name="chevron-right" />
        </button>
      </div>

      <div class="fd-how-dock">
        <div class="fd-how-segs" role="group" aria-label="Steps">
          <button
            v-for="(step, index) in STEPS"
            :key="step.title"
            class="fd-how-seg"
            :class="{ 'is-active': index === active, 'is-done': index < active, 'is-playing': index === playing }"
            :style="{ '--how-step': `${step.ms}ms` }"
            type="button"
            :aria-current="index === active ? 'step' : undefined"
            @click="show(index)"
          >
            <span class="fd-how-seg-bar">
              <span :key="runs[index]" class="fd-how-fill" @animationend.self="show(active + 1)"></span>
            </span>
            <span class="fd-how-seg-label">
              <span class="fd-how-seg-num">{{ stepNumber(index) }}</span>
              <span class="fd-how-seg-title">{{ step.title }}</span>
            </span>
          </button>
        </div>
        <button
          class="btn btn-fd-ghost fd-how-play"
          type="button"
          :aria-label="userPaused ? 'Play the steps' : 'Pause the steps'"
          @click="togglePause"
        >
          <AppIcon :name="userPaused ? 'play' : 'pause'" :size="18" />
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.fd-how {
  position: relative;
  padding: 4.5rem 0 5rem;
  overflow: hidden;
}
.fd-how::before {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    radial-gradient(48rem 26rem at 50% 56%, var(--fd-glow-1), transparent 64%),
    radial-gradient(30rem 16rem at 50% 100%, var(--fd-glow-2), transparent 64%);
  opacity: 0.55;
}
.fd-how > .container {
  position: relative;
}

.fd-how-head {
  max-width: 44rem;
  margin: 0 auto 2.25rem;
  text-align: center;
}
.fd-how-lead {
  max-width: 34rem;
  margin: 0.9rem auto 0;
  color: var(--fd-muted);
  font-size: 1.05rem;
  line-height: 1.6;
}

/* ---- The reel: the current step in the middle, its neighbours peeking ---- */

.fd-how-reel {
  position: relative;
  container-type: inline-size;
  /* Resolved where used, so the cqw are the reel's width. */
  --card: min(72cqw, 880px);
  --gap: 2cqw;
  --peek: calc((100cqw - var(--card)) / 2);
}
.fd-how-viewport {
  overflow: hidden;
  padding: 0.5rem 0 1.5rem;
  touch-action: pan-y;
  -webkit-user-select: none;
  user-select: none;
  -webkit-mask-image: linear-gradient(90deg, transparent 0, #000 6%, #000 94%, transparent 100%);
  mask-image: linear-gradient(90deg, transparent 0, #000 6%, #000 94%, transparent 100%);
}
.fd-how-viewport.is-dragging {
  cursor: grabbing;
}
.fd-how-track {
  display: flex;
  gap: var(--gap);
  transform: translateX(calc(var(--peek) - var(--i, 0) * (var(--card) + var(--gap)) + var(--drag, 0px)));
  transition: transform 0.85s cubic-bezier(0.7, 0, 0.2, 1);
}
.is-dragging .fd-how-track {
  transition: none;
}
.fd-how-card {
  position: relative;
  flex: 0 0 var(--card);
  min-width: 0;
  opacity: 0.38;
  transform: scale(0.9);
  transition:
    transform 0.85s cubic-bezier(0.7, 0, 0.2, 1),
    opacity 0.6s ease;
}
.fd-how-card.is-active {
  opacity: 1;
  transform: none;
}
.fd-how-frame {
  position: relative;
  padding: 5px;
  border: 1px solid var(--fd-border-strong);
  border-radius: var(--fd-radius);
  background: var(--fd-bg-elev);
  box-shadow: var(--fd-card-shadow);
}
.is-active .fd-how-frame {
  border-color: transparent;
  background:
    linear-gradient(var(--fd-bg-elev), var(--fd-bg-elev)) padding-box,
    linear-gradient(135deg, rgba(139, 92, 246, 0.75), rgba(236, 72, 153, 0.45), rgba(249, 115, 22, 0.35))
      border-box;
}
.fd-how-slide {
  position: relative;
  aspect-ratio: 5 / 3;
  overflow: hidden;
  border-radius: 15px;
  background: #07090f;
  container-type: inline-size;
}
.fd-how-badge {
  position: absolute;
  top: 18px;
  right: 18px;
  z-index: 2;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  background: rgba(7, 9, 15, 0.78);
  border: 1px solid rgba(255, 255, 255, 0.16);
  color: #eef0f6;
  font-family: var(--fd-font-mono);
  font-size: 0.7rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.fd-how-cap {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  column-gap: 1rem;
  padding: 1.25rem 0.5rem 0;
}
.fd-how-cap-num {
  font-family: var(--fd-font-mono);
  font-size: 1.35rem;
  font-weight: 800;
  line-height: 1.3;
  background: var(--fd-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.fd-how-cap-title {
  margin: 0;
  font-family: var(--fd-font-mono);
  font-size: 1.1rem;
  font-weight: 700;
  letter-spacing: -0.01em;
  line-height: 1.45;
}
.fd-how-cap-text {
  max-width: 36rem;
  margin: 0.25rem 0 0;
  color: var(--fd-muted);
  font-size: 0.95rem;
  line-height: 1.6;
}
.fd-how-peek {
  position: absolute;
  inset: 0;
  z-index: 3;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: pointer;
}

/* Centred on the current card's picture: slide height is (card - 12px) * 0.6. */
.fd-how-arrow {
  position: absolute;
  top: calc(var(--card) * 0.3 - 16px);
  z-index: 4;
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  padding: 0;
  border: 1px solid var(--fd-border-strong);
  border-radius: 50%;
  background: var(--fd-nav-bg);
  color: var(--fd-ink);
  box-shadow: var(--fd-card-shadow);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
  cursor: pointer;
  transition: transform 0.15s ease;
}
.fd-how-arrow:hover {
  transform: scale(1.06);
}
.fd-how-arrow.is-prev {
  left: calc(var(--peek) - 26px);
}
.fd-how-arrow.is-next {
  right: calc(var(--peek) - 26px);
}
.fd-how-arrow:focus-visible,
.fd-how-seg:focus-visible {
  outline: 2px solid var(--fd-pink);
  outline-offset: 3px;
}

/* ---- The step bar: progress and navigation in one ---- */

.fd-how-dock {
  display: flex;
  align-items: flex-start;
  justify-content: center;
  gap: 1.25rem;
  max-width: 820px;
  margin: 1.5rem auto 0;
}
.fd-how-segs {
  flex: 1;
  min-width: 0;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.75rem;
}
.fd-how-seg {
  min-height: 44px;
  padding: 0.6rem 0.125rem 0.375rem;
  border: 0;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.fd-how-seg-bar {
  display: block;
  height: 3px;
  overflow: hidden;
  border-radius: 3px;
  background: var(--fd-border-strong);
}
.fd-how-fill {
  display: block;
  height: 100%;
  background: var(--fd-gradient);
  transform: scaleX(0);
  transform-origin: left center;
}
.is-done .fd-how-fill {
  opacity: 0.45;
  transform: none;
}
.is-active .fd-how-fill {
  transform: none;
}
.is-playing .fd-how-fill {
  animation: fd-how-progress var(--how-step) linear both;
}
.is-holding .fd-how-fill,
.hs-frozen .fd-how-fill {
  animation-play-state: paused;
}
.fd-how-seg-label {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin-top: 0.6rem;
  color: var(--fd-muted);
  font-family: var(--fd-font-mono);
  font-size: 0.8rem;
  line-height: 1.3;
  transition: color 0.25s ease;
}
.fd-how-seg-num {
  font-weight: 800;
}
.fd-how-seg-title {
  overflow: hidden;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.is-active .fd-how-seg-label,
.fd-how-seg:hover .fd-how-seg-label {
  color: var(--fd-ink);
}
.fd-how-play {
  flex: none;
  width: 44px;
  height: 44px;
  display: inline-grid;
  place-items: center;
  padding: 0;
}

@keyframes fd-how-progress {
  from {
    transform: scaleX(0);
  }
  to {
    transform: none;
  }
}

/* Phones: a wider card running edge to edge, swipe instead of arrows. The
   neighbours keep their full size here, or the slivers that show there is
   more would shrink away. */
@media (max-width: 767.98px) {
  .fd-how {
    padding: 3.5rem 0 4rem;
  }
  .fd-how-reel {
    --card: 84cqw;
    --gap: 2.5cqw;
    margin-inline: calc(var(--bs-gutter-x, 1.5rem) * -0.5);
  }
  .fd-how-card {
    transform: none;
  }
  .fd-how-viewport {
    -webkit-mask-image: none;
    mask-image: none;
  }
  .fd-how-arrow {
    display: none;
  }
  .fd-how-cap {
    column-gap: 0.75rem;
    padding-top: 1rem;
  }
  .fd-how-cap-text {
    font-size: 0.9rem;
  }
  .fd-how-dock {
    gap: 0.75rem;
    margin-top: 1rem;
  }
}
@media (max-width: 575.98px) {
  .fd-how-frame {
    padding: 3px;
    border-radius: 14px;
  }
  .fd-how-slide {
    border-radius: 11px;
  }
  .fd-how-badge {
    top: 10px;
    right: 10px;
  }
  /* Numbers only; the titles stay for screen readers. */
  .fd-how-seg-title {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
}

@media (prefers-reduced-motion: reduce) {
  .fd-how-track,
  .fd-how-card {
    transition: none;
  }
  .is-playing .fd-how-fill {
    animation: none;
  }
  .fd-how-play {
    display: none;
  }
}
</style>
