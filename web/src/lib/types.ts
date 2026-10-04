// Shapes of the server API (docs/api.md). Amounts are decimal strings of base units.

export type Severity = 'watch' | 'warning' | 'critical';
export type SignalLevel = 'ok' | 'warning' | 'critical' | 'unavailable';
export type PositionStatus = 'unprotected' | 'guarded' | 'exiting' | 'exited';
export type TargetType = 'ERC4626' | 'AAVE_V3';

export interface Explorer {
	name: string;
	txUrl: string;
	addressUrl: string;
}

export interface ConfigTarget {
	id: string;
	label: string;
	protocol: string;
	type: TargetType;
	address: `0x${string}`;
	positionToken: `0x${string}`;
	asset: `0x${string}`;
	assetSymbol: string;
	assetDecimals: number;
}

export interface SignalInfo {
	id: string;
	label: string;
	tooltip: string;
}

export interface AppConfig {
	chainId: number;
	chainName: string;
	demoMode: boolean;
	factory: `0x${string}`;
	explorer: Explorer | null;
	telegramEnabled: boolean;
	telegramBot: string | null;
	emailEnabled: boolean;
	defaultTipCapUsd: number;
	targets: ConfigTarget[];
	signals: SignalInfo[];
}

export type OnCritical = 'auto_exit' | 'ask_first';
export type OnWarning = 'notify' | 'exit_half' | 'exit_full';

export interface Policy {
	onCritical: OnCritical;
	onWarning: OnWarning;
	sendTo: 'wallet_usdc';
	priorityExit: boolean;
	tipCapUsd: number;
}

export interface ExitTx {
	hash: `0x${string}`;
	block: number;
	maxPriorityFeePerGasWei: string;
	tipUsd: number;
	amountOut: string;
	burned: string;
	remaining: string;
	result: 'exited' | 'deferred' | 'reverted' | 'pending' | 'replaced';
}

export interface Exit {
	id: number;
	guard: `0x${string}`;
	owner: `0x${string}`;
	targetId: string;
	trigger: 'auto' | 'manual_keeper' | 'owner';
	reason: string;
	reasonHash: `0x${string}`;
	severity: Severity;
	status: 'active' | 'complete' | 'stopped' | 'timeout' | 'failed';
	decidedAt: string;
	decisionToBroadcastMs: number | null;
	tipCapUsd: number;
	txs: ExitTx[];
	totalOut: string;
	startBlock: number;
	endBlock: number | null;
}

export interface Position {
	targetId: string;
	status: PositionStatus;
	walletAmount: string;
	walletPositionTokens: string;
	guardedAmount: string;
	guardedPositionTokens: string;
	exitableAmount: string;
	returnedAmount: string;
	/** What the wallet could withdraw from the protocol right now; null when unknown. */
	walletWithdrawable: string | null;
	severity: Severity;
	policy: Policy | null;
	lastExit: Exit | null;
}

export interface PositionsResponse {
	address: `0x${string}`;
	guard: `0x${string}` | null;
	guardPredicted: `0x${string}`;
	keeperEnabled: boolean;
	paused: boolean;
	positions: Position[];
}

export interface SignalReading {
	id: string;
	level: SignalLevel;
	value: number;
	unit: string;
	detail: string;
}

export interface SignalsSnapshot {
	targetId: string;
	severity: Severity;
	reason: string;
	updatedAt: string;
	block: number;
	signals: SignalReading[];
}

export interface HistoryPoint {
	block: number;
	time: string;
	severity: Severity;
	signals: { id: string; level: SignalLevel; value: number }[];
}

export interface SignalHistory {
	points: HistoryPoint[];
	thresholds: Record<string, { warning?: number; critical?: number }>;
}

export type EventKind =
	| 'check'
	| 'severity'
	| 'alert'
	| 'exit_submitted'
	| 'exit_partial'
	| 'exit_complete'
	| 'exit_deferred'
	| 'exit_failed'
	| 'guard'
	| 'sim';

export interface HeimdallEvent {
	id: number;
	time: string;
	block: number;
	kind: EventKind;
	targetId: string | null;
	guard: `0x${string}` | null;
	severity: 'warning' | 'critical' | 'watch' | null;
	message: string;
	txHash: `0x${string}` | null;
	exitId: number | null;
}

export interface SimLine {
	runId: number;
	line: string;
	done: boolean;
}

export interface SimScenario {
	id: string;
	name: string;
	description: string;
	available: boolean;
	reason: string | null;
}

export interface SimScenarios {
	label: string;
	scenarios: SimScenario[];
	running: string | null;
}

export interface SimParty {
	address: `0x${string}`;
	inWallet: string;
	inVault: string;
	withdrawableNow: string;
}

export interface SimCompare {
	label: string;
	targetId: string;
	ada: SimParty;
	ben: SimParty;
	timeline: {
		firstSignalBlock: number | null;
		exitSubmittedBlock: number | null;
		exitConfirmedBlock: number | null;
		drainFinishedBlock: number | null;
	};
}

export interface UserSettings {
	address: `0x${string}`;
	telegram: { linked: boolean; username: string | null } | null;
	email: { address: string; verified: boolean } | null;
	defaultPolicy: Policy;
}

export interface TelegramLink {
	code: string;
	deepLink: string;
	expiresAt: string;
}

export interface TxInfo {
	hash: `0x${string}`;
	blockNumber: number;
	from: `0x${string}`;
	to: `0x${string}` | null;
	status: 'success' | 'reverted';
	gasUsed: string;
	effectiveGasPrice: string;
	maxPriorityFeePerGas: string;
	logs: { address: `0x${string}`; name: string | null; args: Record<string, unknown> | null }[];
}

export interface NonceResponse {
	nonce: string;
	message: string;
}

export interface VerifyResponse {
	token: string;
	address: `0x${string}`;
	expiresAt: string;
}

export interface BacktestSummary {
	id: string;
	title: string;
	incident: string;
	fromBlock: number;
	toBlock: number;
	generatedAt: string;
}

export interface BacktestPoint {
	block: number;
	time: string;
	totalAssets: string;
	balances: Record<string, string>;
	sharePrice: string;
	outflowPct: number;
	shareDropPct: number;
	severity: Severity;
	levels: Record<string, SignalLevel>;
}

export interface Backtest {
	id: string;
	title: string;
	incident: string;
	chainId: number;
	mode: 'erc4626' | 'balance';
	tokens: string[];
	/** Optional: each token's on-chain symbol(), shown instead of its address. */
	tokenSymbols?: Record<string, string>;
	tokenDecimals: Record<string, number>;
	target: string;
	asset: string;
	assetDecimals: number;
	fromBlock: number;
	toBlock: number;
	step: number;
	generatedAt: string;
	rpcHost: string;
	thresholds: Record<string, { warning?: number; critical?: number }>;
	note: string;
	points: BacktestPoint[];
	firstWarningBlock: number | null;
	firstCriticalBlock: number | null;
	peakTotalAssets: string;
	peakBalances: Record<string, string>;
	totalAssetsAtFirstCritical: string | null;
	pctOfPeakRemainingAtFirstCritical: number | null;
}
