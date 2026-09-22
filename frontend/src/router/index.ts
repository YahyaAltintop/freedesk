import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { RouteName } from '@/constants/routes'
import { useAuthStore } from '@/stores/auth.store'
import HomePage from '@/pages/HomePage.vue'

// No login and no guarded areas: every page works with the invisible anonymous
// identity, so routing is just Home (pair by code) and Connect (the session).
// The home page is imported statically: it is what every visit paints first,
// and a lazy chunk for it only added a second fetch to the first paint.
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: RouteName.Home,
    component: HomePage,
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

// Only the session page needs the identity before it mounts: it creates the
// session in onMounted and relies on authStore.uid / authStore.error being
// settled. The home page does not, and holding it behind the sign-in too would
// put a network round trip in front of the first paint for nothing.
router.beforeEach(async (to) => {
  if (to.name === RouteName.Connect) {
    const authStore = useAuthStore()
    if (!authStore.initialized) {
      await authStore.init()
    }
  }
  return true
})

export default router
