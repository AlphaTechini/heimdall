/** One shared "now" that ticks each second so relative times ("3s ago") stay honest. */
export const clock = $state({ now: Date.now() });

let timer: ReturnType<typeof setInterval> | null = null;

export function startClock() {
	if (timer !== null) return;
	timer = setInterval(() => {
		clock.now = Date.now();
	}, 1000);
}
