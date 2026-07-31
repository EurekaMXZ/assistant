const testGlobal = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean;
};

testGlobal.IS_REACT_ACT_ENVIRONMENT = true;

if (
  typeof HTMLElement !== "undefined" &&
  typeof HTMLElement.prototype.getAnimations !== "function"
) {
  HTMLElement.prototype.getAnimations = () => [];
}

if (typeof globalThis.requestAnimationFrame !== "function") {
  globalThis.requestAnimationFrame = (callback) =>
    window.setTimeout(() => callback(performance.now()), 0);
}

if (typeof globalThis.cancelAnimationFrame !== "function") {
  globalThis.cancelAnimationFrame = (frame) => window.clearTimeout(frame);
}
