#!/usr/bin/env node
/**
 * Convert YAML to JSON.
 * Usage: node yaml-to-json.mjs input.yaml output.json
 */
import { readFileSync, writeFileSync } from 'fs';
import { parse } from 'yaml';

const [,, inputPath, outputPath] = process.argv;

if (!inputPath || !outputPath) {
  console.error('Usage: node yaml-to-json.mjs input.yaml output.json');
  process.exit(1);
}

try {
  const yamlContent = readFileSync(inputPath, 'utf8');
  const jsonData = parse(yamlContent);
  writeFileSync(outputPath, JSON.stringify(jsonData, null, 2), 'utf8');
  console.log(`Converted ${inputPath} to ${outputPath}`);
} catch (error) {
  console.error('Error converting YAML to JSON:', error.message);
  process.exit(1);
}
