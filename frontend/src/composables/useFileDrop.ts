import { onScopeDispose, readonly, ref, watch, type Ref } from 'vue'

export interface FileDrop {
  // True while files are being dragged over the stage.
  dragging: Readonly<Ref<boolean>>
  // Set when a drop contained something that cannot be sent, e.g. a folder.
  refusal: Ref<string | null>
}

// Accepts files dropped onto the session stage.
//
// Drag events are a separate family from pointer events: dragging a file in
// from the desktop fires no pointerdown or pointermove, so none of the
// pointer-capture or click-to-take-control logic in useInputCapture is
// disturbed by this.
//
// Listeners go on the whole stage, not just the video. A file dropped on the
// toolbar or on the black bars beside a letterboxed screen would otherwise hit
// the browser's own default, which is to navigate to the file — ending the
// session outright.
export function useFileDrop(
  target: Ref<HTMLElement | null>,
  onFiles: (files: File[]) => void,
  enabled: Ref<boolean>,
): FileDrop {
  const dragging = ref(false)
  const refusal = ref<string | null>(null)
  // dragenter/dragleave fire per element, so crossing into a child looks like
  // leaving. Counting them is what keeps the overlay from flickering.
  let depth = 0
  let el: HTMLElement | null = null

  function carriesFiles(event: DragEvent): boolean {
    return event.dataTransfer?.types.includes('Files') ?? false
  }

  const onDragEnter = (event: DragEvent): void => {
    if (!carriesFiles(event)) {
      return
    }
    event.preventDefault()
    depth++
    if (enabled.value) {
      dragging.value = true
    }
  }

  const onDragOver = (event: DragEvent): void => {
    if (!carriesFiles(event)) {
      return
    }
    // Without this the drop event never fires and the browser navigates away.
    event.preventDefault()
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = enabled.value ? 'copy' : 'none'
    }
  }

  const onDragLeave = (): void => {
    depth = Math.max(0, depth - 1)
    if (depth === 0) {
      dragging.value = false
    }
  }

  const onDrop = (event: DragEvent): void => {
    event.preventDefault()
    reset()
    if (!enabled.value) {
      return
    }

    const items = Array.from(event.dataTransfer?.items ?? [])
    // A dropped folder arrives as a zero-byte entry that fails on read, and the
    // browser will not hand over its contents. Refusing plainly beats
    // half-handling it.
    const hasFolder = items.some((i) => i.webkitGetAsEntry()?.isDirectory)
    const files = Array.from(event.dataTransfer?.files ?? []).filter((f) => f.size > 0)

    if (hasFolder) {
      refusal.value = "Folders can't be sent yet — drop the files inside."
    }
    if (files.length > 0) {
      if (!hasFolder) {
        refusal.value = null
      }
      onFiles(files)
    }
  }

  // A drag that leaves the window never delivers a drop, and the counter would
  // otherwise strand the overlay on screen.
  const onDragEnd = (): void => reset()

  function reset(): void {
    depth = 0
    dragging.value = false
  }

  function attach(node: HTMLElement): void {
    detach()
    el = node
    node.addEventListener('dragenter', onDragEnter)
    node.addEventListener('dragover', onDragOver)
    node.addEventListener('dragleave', onDragLeave)
    node.addEventListener('drop', onDrop)
    window.addEventListener('dragend', onDragEnd)
    window.addEventListener('blur', onDragEnd)
  }

  function detach(): void {
    if (el) {
      el.removeEventListener('dragenter', onDragEnter)
      el.removeEventListener('dragover', onDragOver)
      el.removeEventListener('dragleave', onDragLeave)
      el.removeEventListener('drop', onDrop)
    }
    window.removeEventListener('dragend', onDragEnd)
    window.removeEventListener('blur', onDragEnd)
    reset()
    el = null
  }

  watch(target, (node) => (node ? attach(node) : detach()), { immediate: true })
  onScopeDispose(detach)

  return { dragging: readonly(dragging), refusal }
}
