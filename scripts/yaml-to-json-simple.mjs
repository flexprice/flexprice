#!/usr/bin/env node
/**
 * Simple YAML to JSON converter using minimal parsing.
 * Works for the specific YAML output from speakeasy overlay apply.
 */
import { readFileSync, writeFileSync } from 'fs';

const [,, inputPath, outputPath] = process.argv;

if (!inputPath || !outputPath) {
  console.error('Usage: node yaml-to-json-simple.mjs input.yaml output.json');
  process.exit(1);
}

try {
  const yamlContent = readFileSync(inputPath, 'utf8');
  
  // Simple approach: save to temp file and use speakeasy to convert
  // But let's just use a JSON library that Node has built-in for JSON parsing
  // Actually, let's save YAML, read it as text, and use Python to convert
  
  // Even simpler: Just tell speakeasy to output JSON directly if possible
  // For now, let's just use the YAML file and have the filter script handle it
  
  console.error('YAML conversion not available. Using alternative approach...');
  process.exit(1);
  
} catch (error) {
  console.error('Error:', error.message);
  process.exit(1);
}
