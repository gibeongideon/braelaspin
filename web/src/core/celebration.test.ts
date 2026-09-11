import { describe, it, expect } from 'vitest';
import { celebrationFor, confettiColours, type Tier } from './celebration';

describe('celebration tiers', () => {
  it('escalates with the multiplier', () => {
    expect(celebrationFor('win', 20_000).tier).toBe('small');    // 2x
    expect(celebrationFor('win', 50_000).tier).toBe('big');      // 5x
    expect(celebrationFor('win', 100_000).tier).toBe('big');     // 10x
    expect(celebrationFor('win', 500_000).tier).toBe('huge');    // 50x
    expect(celebrationFor('win', 2_000_000).tier).toBe('jackpot'); // 200x
  });

  it('treats a loss quietly — never loud', () => {
    const c = celebrationFor('loss', 0);
    expect(c.tier).toBe('loss');
    expect(c.confetti).toBe(0);
    expect(c.rays).toBe(false);
    expect(c.chime).toEqual([]);
    // Off the screen quickly; a loss should not be dwelt on.
    expect(c.dismissMs).toBeLessThan(2000);
  });

  it('a 1x refund is neither a win nor a loss', () => {
    const c = celebrationFor('refund', 10_000);
    expect(c.tier).toBe('refund');
    expect(c.title).toBe('Bet returned');
    expect(c.confetti).toBe(0);
  });

  it('confetti and dismiss time rise monotonically with the tier', () => {
    const order: Tier[] = ['loss', 'refund', 'small', 'big', 'huge', 'jackpot'];
    const cs = [
      celebrationFor('loss', 0),
      celebrationFor('refund', 10_000),
      celebrationFor('win', 20_000),
      celebrationFor('win', 50_000),
      celebrationFor('win', 500_000),
      celebrationFor('win', 2_000_000),
    ];
    expect(cs.map((c) => c.tier)).toEqual(order);
    for (let i = 1; i < cs.length; i++) {
      expect(cs[i]!.confetti).toBeGreaterThanOrEqual(cs[i - 1]!.confetti);
      expect(cs[i]!.dismissMs).toBeGreaterThanOrEqual(cs[i - 1]!.dismissMs);
    }
  });

  it('chimes rise in pitch, so a bigger win sounds bigger', () => {
    for (const bp of [20_000, 50_000, 500_000, 2_000_000]) {
      const notes = celebrationFor('win', bp).chime;
      for (let i = 1; i < notes.length; i++) {
        expect(notes[i]!).toBeGreaterThan(notes[i - 1]!);
      }
    }
  });

  it('every tier has a usable palette', () => {
    for (const t of ['loss', 'refund', 'small', 'big', 'huge', 'jackpot'] as Tier[]) {
      const cols = confettiColours(t);
      expect(cols.length).toBeGreaterThan(0);
      for (const c of cols) expect(c).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });

  it('titles stay short enough to sit above a large number', () => {
    for (const bp of [0, 10_000, 20_000, 50_000, 500_000, 2_000_000]) {
      const kind = bp === 0 ? 'loss' : bp === 10_000 ? 'refund' : 'win';
      expect(celebrationFor(kind, bp).title.length).toBeLessThanOrEqual(12);
    }
  });
});
