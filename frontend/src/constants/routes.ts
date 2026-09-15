// Route names — referenced by the router and all navigation to avoid magic
// strings and keep links refactor-safe.
export const RouteName = {
  Home: 'home',
  Connect: 'connect',
} as const

export type RouteNameValue = (typeof RouteName)[keyof typeof RouteName]
