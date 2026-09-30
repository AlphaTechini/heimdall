import type { Position, Severity, SignalsSnapshot } from './types';

export type Tone = 'calm' | 'warning' | 'critical' | 'exiting' | 'safe' | 'unknown';

export interface Band {
	word: string;
	tone: Tone;
}

const SEVERITY_BAND: Record<Severity, Band> = {
	watch: { word: 'Calm', tone: 'calm' },
	warning: { word: 'Warning', tone: 'warning' },
	critical: { word: 'Critical', tone: 'critical' }
};

/** The horn band word and color for a position: exit states win, otherwise the target's live severity. */
export function bandFor(position: Position, snapshot: SignalsSnapshot | undefined): Band {
	if (position.status === 'exiting') return { word: 'Exiting', tone: 'exiting' };
	if (position.status === 'exited') return { word: 'Home safe', tone: 'safe' };
	const severity = snapshot?.severity ?? position.severity;
	return SEVERITY_BAND[severity] ?? { word: 'Checking', tone: 'unknown' };
}

/** Higher means worse. Used to sweep the band only when things get worse. */
export function toneRank(tone: Tone): number {
	switch (tone) {
		case 'calm':
			return 0;
		case 'warning':
			return 1;
		case 'critical':
			return 2;
		case 'exiting':
			return 3;
		default:
			return -1;
	}
}

export const STATUS_LABEL: Record<Position['status'], string> = {
	unprotected: 'Unprotected',
	guarded: 'Guarded',
	exiting: 'Exiting',
	exited: 'Exited'
};
