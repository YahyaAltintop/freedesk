import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { RouteName } from '@/constants/routes'
import { useAuthStore } from '@/stores/auth.store'

// No login and no guarded areas: every page works with the invisible anonymous
// identity, so routing is just Home (pair by code) and Connect (the session).
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: RouteName.Home,
    component: () => import('@/pages/HomePage.vue'),
  },
  {
    path: '/connect/:hostId',
    name: RouteName.Connect,
    component: () => import('@/pages/ConnectPage.vue'),
    props: true,
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: { name: RouteName.Home },
  },
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

// Global guard: make sure the anonymous sign-in attempt finished before any
// page mounts, so pages can rely on authStore.uid / authStore.error.
router.beforeEach(async () => {
  const authStore = useAuthStore()
  if (!authStore.initialized) {
    await authStore.init()
  }
  return true
})

export default router
