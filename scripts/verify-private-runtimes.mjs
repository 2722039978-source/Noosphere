import { resolve } from 'node:path';
import { nativeTarget, verifyRuntimes } from './desktop-platform.mjs';

const [target, root] = process.argv.slice(2);
if (!target || !root) throw new Error('Expected a native target and the installed resource directory');
await verifyRuntimes(nativeTarget(target), resolve(root), process.cwd(), true);
