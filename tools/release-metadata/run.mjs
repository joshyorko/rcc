import { writeFile } from 'node:fs/promises';
import { GitHub, reconcile } from './github.mjs';
const write = process.argv.includes('--write');
if (write && process.env.RCC_RELEASE_METADATA_PROPOSE !== 'true') throw new Error('Release metadata proposal is disabled');
const result = await reconcile(new GitHub(process.env.RCC_RELEASE_METADATA_TOKEN), { write, expectedSource: process.env.RCC_RELEASE_EXPECTED_SHA });
// PROPOSE/PREVIEW may contain public release text, never token/private homes.
await writeFile(process.env.RCC_RELEASE_METADATA_RECEIPT || 'release-metadata-receipt.json', JSON.stringify(result, null, 2) + '\n');
console.log(`Release metadata: ${result.status}; publication remains owner-approved`);
