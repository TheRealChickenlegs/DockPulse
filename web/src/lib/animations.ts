/**
 * Tiny wrapper around the Motion library that avoids selector-vs-object
 * overload ambiguity in TypeScript. Resolves a selector to one or more
 * elements before invoking `animate`, so the call site gets strict
 * element typing rather than the union of all `animate` overloads.
 */
import { animate as motionAnimate, inView as motionInView } from 'motion';
import type { AnimationPlaybackControls, AnimationOptions } from 'motion';

export interface Keyframes {
	[x: string]: string | number | (string | number)[];
}

export type AnimateOptions = Partial<AnimationOptions> & Record<string, unknown>;

function resolve(target: Element | Element[] | NodeListOf<Element> | string): Element[] {
	if (typeof target === 'string') {
		return Array.from(document.querySelectorAll(target));
	}
	if (target instanceof NodeList) {
		return Array.from(target);
	}
	if (Array.isArray(target)) {
		return target;
	}
	return [target];
}

export function animateOn(
	target: Element | Element[] | NodeListOf<Element> | string,
	keyframes: Keyframes,
	options: AnimateOptions = {}
): AnimationPlaybackControls[] {
	const elements = resolve(target);
	return elements.map((el) =>
		motionAnimate(el, keyframes as Record<string, string | number | (string | number)[]>, options as AnimationOptions)
	);
}

export function inViewOn(
	target: Element | Element[] | NodeListOf<Element> | string,
	callback: (info: { target: Element }) => void
): () => void {
	const elements = resolve(target);
	return motionInView(elements, (entry: IntersectionObserverEntry) => {
		callback({ target: entry.target });
	});
}

/**
 * Lightweight per-element stagger helper. Pass the index of an element
 * and the per-step duration to get a delay in seconds.
 */
export function staggerDelay(index: number, step = 0.06): number {
	return index * step;
}