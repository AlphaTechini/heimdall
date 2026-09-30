export interface Toast {
	id: number;
	message: string;
}

class ToastStore {
	items = $state<Toast[]>([]);
	private next = 1;

	show(message: string, ms = 6000) {
		const id = this.next++;
		this.items = [...this.items, { id, message }];
		setTimeout(() => this.dismiss(id), ms);
	}

	dismiss(id: number) {
		this.items = this.items.filter((t) => t.id !== id);
	}
}

export const toasts = new ToastStore();
