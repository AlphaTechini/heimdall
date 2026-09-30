import { api } from './api';
import { describeError } from './errors';
import type { AppConfig, ConfigTarget } from './types';

/** Server configuration (GET /config), loaded once at start. */
class AppStore {
	status = $state<'loading' | 'ready' | 'error'>('loading');
	config = $state<AppConfig | null>(null);
	error = $state('');

	async load() {
		this.status = 'loading';
		try {
			this.config = await api.config();
			this.status = 'ready';
			this.error = '';
		} catch (e) {
			this.error = describeError(e, 'Could not load the Heimdall settings.').message;
			this.status = 'error';
		}
	}

	target(id: string): ConfigTarget | undefined {
		return this.config?.targets.find((t) => t.id === id);
	}

	signalInfo(id: string) {
		return this.config?.signals.find((s) => s.id === id);
	}
}

export const app = new AppStore();
