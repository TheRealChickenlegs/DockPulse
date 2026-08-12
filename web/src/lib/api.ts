/**
 * Thin typed wrapper around the DockPulse API. The base URL is
 * always relative (the controller is served on the same origin as
 * the SPA) so this module never needs to know the public hostname.
 */

import { resolve } from '$app/paths';
import { PUBLIC_CONTROLLER_BASE } from '$env/static/public';

const base = PUBLIC_CONTROLLER_BASE || '';

export class ApiError extends Error {
	status: number;
	body: unknown;
	constructor(status: number, message: string, body: unknown) {
		super(message);
		this.status = status;
		this.body = body;
	}
}

function readCookie(name: string): string | null {
	if (typeof document === 'undefined') return null;
	const m = document.cookie.match(new RegExp('(^|; )' + name + '=([^;]*)'));
	return m ? decodeURIComponent(m[2]) : null;
}

interface RequestOptions {
	method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
	body?: unknown;
	signal?: AbortSignal;
	auth?: boolean; // include CSRF header for mutating requests
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
	const method = opts.method ?? 'GET';
	const headers: Record<string, string> = {
		Accept: 'application/json'
	};
	if (opts.body !== undefined) {
		headers['Content-Type'] = 'application/json';
	}
	if (opts.auth && (method === 'POST' || method === 'PUT' || method === 'PATCH' || method === 'DELETE')) {
		const csrf = readCookie('dockpulse_csrf');
		if (csrf) {
			headers['X-CSRF-Token'] = csrf;
		}
	}

	const res = await fetch(base + path, {
		method,
		headers,
		credentials: 'include',
		body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
		signal: opts.signal
	});

	const text = await res.text();
	let parsed: unknown = null;
	if (text) {
		try {
			parsed = JSON.parse(text);
		} catch {
			parsed = text;
		}
	}

	if (!res.ok) {
		const message =
			(parsed && typeof parsed === 'object' && 'error' in parsed && typeof (parsed as { error: unknown }).error === 'string')
				? (parsed as { error: string }).error
				: `HTTP ${res.status}`;
		throw new ApiError(res.status, message, parsed);
	}
	return parsed as T;
}

export interface User {
	id: string;
	username: string;
	email: string;
	role: 'admin' | 'user';
	created_at: string;
	last_login_at?: string;
	disabled: boolean;
}

export interface MeResponse {
	user: User;
	csrf_token: string;
	first_run_hint: boolean;
}

export interface FirstRunStatus {
	needs_setup: boolean;
}

export const api = {
	firstRunStatus: () => request<FirstRunStatus>('/api/v1/firstrun'),
	firstRunCreate: (body: { username: string; password: string; email: string }) =>
		request<{ user: User }>('/api/v1/firstrun', { method: 'POST', body }),
	login: (body: { username: string; password: string }) =>
		request<{ user: User }>('/api/v1/login', { method: 'POST', body }),
	logout: () => request<{ ok: boolean }>('/api/v1/logout', { method: 'POST', auth: true }),
	me: () => request<MeResponse>('/api/v1/me')
};

export const urls = {
	dashboard: () => resolve('/'),
	login: () => resolve('/login'),
	servers: () => resolve('/servers'),
	containers: () => resolve('/containers'),
	updates: () => resolve('/updates'),
	settings: () => resolve('/settings')
};
