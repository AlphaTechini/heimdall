// `$env/static/public` is inlined at build time, which is what adapter-static needs
// (a static SPA has no server to hand out runtime env). The namespace import keeps the build
// working when PUBLIC_API_URL is not set.
import * as publicEnv from '$env/static/public';

const configured = (publicEnv as Record<string, string | undefined>).PUBLIC_API_URL;

export const API_URL = (
	configured && configured.trim() ? configured.trim() : 'http://127.0.0.1:8080'
).replace(/\/+$/, '');

export const STREAM_URL = API_URL.replace(/^http/, 'ws') + '/stream';
