declare module "bun:test" {
  interface Matchers<T> {
    toBe(expected: T): void;
    toEqual(expected: unknown): void;
    toStrictEqual(expected: unknown): void;
    toBeNull(): void;
    toBeUndefined(): void;
    toBeDefined(): void;
    toBeTruthy(): void;
    toBeFalsy(): void;
    toContain(expected: unknown): void;
    toHaveLength(expected: number): void;
    toBeGreaterThan(expected: number): void;
    toBeGreaterThanOrEqual(expected: number): void;
    toBeLessThan(expected: number): void;
    toBeLessThanOrEqual(expected: number): void;
    toMatch(expected: string | RegExp): void;
    toThrow(expected?: string | RegExp | Error): void;
    readonly not: Matchers<T>;
  }

  export function describe(name: string, fn: () => void): void;
  export function test(name: string, fn: () => void | Promise<void>): void;
  export function it(name: string, fn: () => void | Promise<void>): void;
  export function beforeEach(fn: () => void | Promise<void>): void;
  export function afterEach(fn: () => void | Promise<void>): void;
  export function expect<T>(actual: T): Matchers<T>;
}
