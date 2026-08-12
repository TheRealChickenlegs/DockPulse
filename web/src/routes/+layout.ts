// Disable SSR — the controller serves the bundle as static files and
// we want fully client-side routing so the SPA shell matches a future
// mobile companion shell.
export const prerender = true;
export const ssr = false;