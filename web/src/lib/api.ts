import { API_URL } from './env';
import { AppError } from './errors';
import type {
	AppConfig,
	Exit,
	HeimdallEvent,
	Policy,
	PositionsResponse,
	SignalHistory,
	SignalsSnapshot,
	SimCompare,
	SimScenarios,
	TelegramLink,
	TxInfo,
	UserSettings,
	NonceResponse,
	VerifyResponse
} from './types';

export class ApiError extends AppError {
	status: number;
	constructor(status: number, message: string) {
		super(status === 401 ? 'auth' : 'server', message);
		this.name = 'ApiError';
		this.status = status;
	}
}

interface RequestOptions {
	method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
	body?: unknown;
	token?: string | null;
	signal?: AbortSignal;
}

const TIMEOUT_MS = 10_000;

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
	if (opts.token) headers.Authorization = `Bearer ${opts.token}`;

	const timeout = AbortSignal.timeout(TIMEOUT_MS);
	const signal = opts.signal ? AbortSignal.any([opts.signal, timeout]) : timeout;

	let res: Response;
	try {
		res = await fetch(API_URL + path, {
			method: opts.method ?? 'GET',
			headers,
			body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
			signal
		});
	} catch (e) {
		if (opts.signal?.aborted) throw e;
		throw new ApiError(
			0,
			`Can't reach the Heimdall server at ${API_URL}. Check that it is running, then try again.`
		);
	}

	if (res.status === 204) return undefined as T;
	const payload: unknown = await res.json().catch(() => null);
	if (!res.ok) {
		const fromServer =
			payload && typeof payload === 'object' && 'error' in payload
				? String((payload as { error: unknown }).error)
				: '';
		throw new ApiError(
			res.status,
			fromServer ||
				`The Heimdall server answered with an error (${res.status}). Try again in a moment.`
		);
	}
	return payload as T;
}

const q = (params: Record<string, string | number | null | undefined>) => {
	const sp = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v !== null && v !== undefined && v !== '') sp.set(k, String(v));
	}
	const s = sp.toString();
	return s ? `?${s}` : '';
};

export const api = {
	config: () => request<AppConfig>('/config'),
	positions: (address: string, signal?: AbortSignal) =>
		request<PositionsResponse>(`/positions${q({ address })}`, { signal }),
	signals: (targetId: string, signal?: AbortSignal) =>
		request<SignalsSnapshot>(`/signals/${encodeURIComponent(targetId)}`, { signal }),
	history: (targetId: string, limit = 200, signal?: AbortSignal) =>
		request<SignalHistory>(`/signals/${encodeURIComponent(targetId)}/history${q({ limit })}`, {
			signal
		}),
	activity: (
		params: { address?: string | null; targetId?: string | null; limit?: number },
		signal?: AbortSignal
	) => request<{ events: HeimdallEvent[] }>(`/activity${q(params)}`, { signal }),
	exit: (id: number, signal?: AbortSignal) => request<Exit>(`/exits/${id}`, { signal }),
	tx: (hash: string, signal?: AbortSignal) =>
		request<TxInfo>(`/tx/${encodeURIComponent(hash)}`, { signal }),

	nonce: (address: string) => request<NonceResponse>(`/auth/nonce${q({ address })}`),
	verify: (body: { address: string; message: string; signature: string }) =>
		request<VerifyResponse>('/auth/verify', { method: 'POST', body }),

	putPolicy: (token: string, guard: string, targetId: string, policy: Policy) =>
		request<Policy>(`/policies/${guard}/${encodeURIComponent(targetId)}`, {
			method: 'PUT',
			body: policy,
			token
		}),
	settings: (token: string) => request<UserSettings>('/me/settings', { token }),
	putDefaultPolicy: (token: string, policy: Policy) =>
		request<Policy>('/me/default-policy', { method: 'PUT', body: policy, token }),
	telegramLink: (token: string) =>
		request<TelegramLink>('/me/telegram/link', { method: 'POST', token }),
	telegramDisconnect: (token: string) => request<void>('/me/telegram', { method: 'DELETE', token }),
	telegramTest: (token: string) =>
		request<{ sent: boolean }>('/me/telegram/test', { method: 'POST', token }),
	emailStart: (token: string, email: string) =>
		request<{ sent: boolean }>('/me/email', { method: 'POST', body: { email }, token }),
	emailVerify: (token: string, code: string) =>
		request<{ verified: boolean }>('/me/email/verify', { method: 'POST', body: { code }, token }),
	emailTest: (token: string) =>
		request<{ sent: boolean }>('/me/email/test', { method: 'POST', token }),

	simScenarios: () => request<SimScenarios>('/sim/scenarios'),
	simRun: (id: string) =>
		request<{ runId: number }>(`/sim/scenarios/${encodeURIComponent(id)}/run`, { method: 'POST' }),
	simReset: () => request<void>('/sim/reset', { method: 'POST' }),
	simCompare: () => request<SimCompare>('/sim/compare')
};
