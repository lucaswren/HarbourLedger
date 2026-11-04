import test from 'node:test'
import assert from 'node:assert/strict'
import { currentMonth, ledgerPeriod } from '../src/period.ts'

test('all months includes newly imported current-month records', () => {
 assert.deepEqual(ledgerPeriod({checks:[{period:'2026-09'}],tx:[{period:'2026-10'}],invoices:[{date:'2026-03-01'}],sales:[],unclassified:[]}), ['2026-03','2026-10'])
})

test('invoices can be viewed before any bank statement is imported', () => {
 assert.deepEqual(ledgerPeriod({checks:[],tx:[],invoices:[],sales:[{date:'2025-12-20'}],unclassified:[{date:'2026-02-01'},{date:''}]}), ['2025-12','2026-02'])
})

test('empty or malformed periods use the supplied local month', () => {
 assert.deepEqual(ledgerPeriod({checks:[{period:'2026-13'}],tx:[],invoices:[{date:'unknown'}],sales:[],unclassified:[]}, '2026-10'), ['2026-10','2026-10'])
 assert.equal(currentMonth(new Date(2026, 0, 1)), '2026-01')
})
