import { writable, derived, type Readable } from 'svelte/store';
import { browser } from '$app/environment';
import { api, type User, type MeResponse } from './api';

interface SessionState {
	loaded: boolean;
	user: User | null;
	csrf: string;
	firstRun: boolean;
}

const initial: SessionState = {
	loaded: false,
	user: null,
	csrf: '',
	firstRun: false
};

function createSessionStore() {
	const store = writable<SessionState>(initial);

	async function refresh() {
		if (!browser) {
			store.set({ ...initial, loaded: true });
			return;
		}
		try {
			const me: MeResponse = await api.me();
			store.set({
				loaded: true,
				user: me.user,
				csrf: me.csrf_token,
				firstRun: me.first_run_hint
			});
		} catch (err) {
			store.set({ ...initial, loaded: true });
		}
	}

	async function logout() {
		try {
			await api.logout();
		} finally {
			store.set({ ...initial, loaded: true });
			if (browser) window.location.href = '/login';
		}
	}

	return {
		subscribe: store.subscribe,
		refresh,
		logout
	};
}

export const session = createSessionStore();

export const isAuthenticated: Readable<boolean> = derived(session, ($s) => !!$s.user);
