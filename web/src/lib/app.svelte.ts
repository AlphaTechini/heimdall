import { api } from './api';
import { describeError } from './errors';
import type { AppConfig, BacktestSummary, ConfigTarget } from './types';

/** Server configuration (GET /config), loaded once at start. */
class AppStore {
	status = $state<'loading' | 'ready' | 'error'>('loading');
	config = $state<AppConfig | null>(null);
	error = $state('');
	/** Incidents with real replay data. The Backtest page and nav item exist only when this is not empty (specs N7). */
	backtests = $state<BacktestSummary[]>([]);
	backtestsLoaded = $state(false);

	async load() {
		this.status = 'loading';
		try {
			this.config = await api.config();
			this.status = 'ready';
			this.error = '';
			void this.loadBacktests();
		} catch (e) {
			this.error = describeError(e, 'Could not load the Heimdall settings.').message;
			this.status = 'error';
		}
	}

	async loadBacktests() {
		try {
			this.backtests = (await api.backtests()).backtests ?? [];
		} catch {
			this.backtests = []; // no data, or an older server: the page stays hidden
		}
		this.backtestsLoaded = true;
	}

	target(id: string): ConfigTarget | undefined {
		return this.config?.targets.find((t) => t.id === id);
	}

	signalInfo(id: string) {
		return this.config?.signals.find((s) => s.id === id);
	}
}

export const app = new AppStore();
