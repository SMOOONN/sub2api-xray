<template>
  <Teleport to="body">
    <Transition name="modal" @after-enter="focusInitialElement">
      <div
        v-if="show"
        class="modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <div ref="dialogRef" :class="['modal-content', widthClasses]" @click.stop>
          <div class="modal-header">
            <h3 :id="dialogId" class="modal-title">
              {{ title }}
            </h3>
            <button
              v-if="showCloseButton"
              class="-mr-2 rounded-xl p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900"
              aria-label="Close modal"
              @click="emit('close')"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <div ref="modalBodyRef" class="modal-body">
            <slot></slot>
          </div>

          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
let dialogIdCounter = 0
const openDialogs = new Set<string>()
</script>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = `modal-title-${++dialogIdCounter}`

const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
let previousActiveElement: HTMLElement | null = null
let pendingFocusFrame: number | null = null

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: false,
  showCloseButton: true,
  zIndex: 50
})

const emit = defineEmits<Emits>()

const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (props.show && props.closeOnEscape && event.key === 'Escape') {
    emit('close')
  }
}

const focusInitialElement = () => {
  if (!props.show || !dialogRef.value) return

  const formControl = dialogRef.value.querySelector<HTMLElement>(
    '[data-dialog-autofocus]:not([disabled]), input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled])'
  )
  const firstFocusable = formControl ?? dialogRef.value.querySelector<HTMLElement>(
    'button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'
  )
  firstFocusable?.focus()
}

const scheduleInitialFocus = async () => {
  await nextTick()
  if (pendingFocusFrame !== null) {
    window.cancelAnimationFrame(pendingFocusFrame)
  }
  pendingFocusFrame = window.requestAnimationFrame(() => {
    pendingFocusFrame = null
    focusInitialElement()
  })
}

const updateScrollLock = (isOpen: boolean) => {
  if (isOpen) openDialogs.add(dialogId)
  else openDialogs.delete(dialogId)
  document.body.classList.toggle('modal-open', openDialogs.size > 0)
}

// Prevent body scroll when modal is open and manage focus
watch(
  () => props.show,
  async (isOpen) => {
    if (isOpen) {
      previousActiveElement = document.activeElement as HTMLElement
      // 使用CSS类而不是直接操作style,更易于管理多个对话框
      updateScrollLock(true)

      // 等待DOM更新后设置焦点到对话框
      await nextTick()
      if (modalBodyRef.value) {
        modalBodyRef.value.scrollTop = 0
      }
      await scheduleInitialFocus()
      return
    }

    if (pendingFocusFrame !== null) {
      window.cancelAnimationFrame(pendingFocusFrame)
      pendingFocusFrame = null
    }
    updateScrollLock(false)
    if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
      previousActiveElement.focus()
    }
    previousActiveElement = null
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscape)
  if (pendingFocusFrame !== null) {
    window.cancelAnimationFrame(pendingFocusFrame)
  }
  updateScrollLock(false)
})
</script>
