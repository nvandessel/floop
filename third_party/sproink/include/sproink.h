#ifndef SPROINK_H
#define SPROINK_H

#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct SproinkGraph SproinkGraph;

typedef struct SproinkPairs SproinkPairs;

typedef struct SproinkResults SproinkResults;

/**
 * Builds a CSR graph from parallel arrays of edge data.
 *
 * Returns a heap-allocated `SproinkGraph` pointer, or null on failure.
 * Free with [`sproink_graph_free()`].
 *
 * # Resource Usage
 *
 * Memory scales as O(num_nodes + num_edges). For very large `num_nodes`
 * values (e.g., > 10M), expect significant memory allocation.
 *
 * # Safety
 *
 * - `sources`, `targets`, `weights`, and `kinds` must all be non-null and point
 *   to arrays of at least `num_edges` elements.
 * - The caller retains no ownership of the input arrays; the graph copies all data.
 */
struct SproinkGraph *sproink_graph_build(uint32_t num_nodes,
                                         uint32_t num_edges,
                                         const uint32_t *sources,
                                         const uint32_t *targets,
                                         const double *weights,
                                         const uint8_t *kinds);

/**
 * Frees a graph previously returned by [`sproink_graph_build()`].
 *
 * # Safety
 *
 * - `graph` must be a pointer returned by `sproink_graph_build()`, or null.
 * - Must not be called more than once on the same pointer.
 *   Passing a previously-freed pointer is undefined behavior.
 */
void sproink_graph_free(struct SproinkGraph *graph);

/**
 * Runs spreading activation on the graph.
 *
 * Returns a heap-allocated `SproinkResults` pointer, or null on failure.
 * Free with [`sproink_results_free()`].
 *
 * # Resource Usage
 *
 * - Memory: O(num_nodes) for activation arrays. For graphs above 1024 nodes,
 *   parallel execution allocates O(threads × 4 × num_nodes) transient memory.
 * - Time: O(num_nodes × num_edges × max_steps) worst case.
 *
 * # Thread Safety
 *
 * `SproinkGraph` is immutable after construction and may be shared across
 * threads. However, each `sproink_activate` call must use its own
 * `SproinkResults` — results are not thread-safe.
 *
 * # Safety
 *
 * - `graph` must be a valid pointer returned by `sproink_graph_build()`.
 * - `seed_nodes` and `seed_activations` must point to arrays of at least
 *   `num_seeds` elements, or be null when `num_seeds == 0`.
 * - `seed_sources` may be null (all seeds get `source: None`) or must point
 *   to an array of at least `num_seeds` elements. Use `u32::MAX` as the
 *   "no source" sentinel for individual seeds.
 * - `temporal_decay_rate` and `current_time` use NaN as the "not set"
 *   sentinel, mapping to `None` in the engine config.
 */
struct SproinkResults *sproink_activate(const struct SproinkGraph *graph,
                                        uint32_t num_seeds,
                                        const uint32_t *seed_nodes,
                                        const double *seed_activations,
                                        const uint32_t *seed_sources,
                                        uint32_t max_steps,
                                        double decay_factor,
                                        double spread_factor,
                                        double min_activation,
                                        double sigmoid_gain,
                                        double sigmoid_center,
                                        uint8_t inhibition_enabled,
                                        double inhibition_strength,
                                        uint32_t inhibition_breadth,
                                        double temporal_decay_rate,
                                        double current_time);

/**
 * Returns the number of results, or 0 if `results` is null.
 *
 * # Safety
 *
 * - `results` must be a valid pointer from `sproink_activate()`, or null.
 */
uint32_t sproink_results_len(const struct SproinkResults *results);

/**
 * Copies result node IDs into `out`.
 *
 * Writes at most `min(buffer_len, sproink_results_len(results))` elements.
 * The number of elements actually written is returned (bounded by both the
 * buffer capacity and the result count).
 *
 * # Safety
 *
 * - `results` must be a valid pointer from `sproink_activate()`.
 * - `out` must point to a buffer of at least `buffer_len` `u32` elements, or
 *   be null (in which case this function is a no-op returning 0).
 */
uint32_t sproink_results_nodes(const struct SproinkResults *results,
                               uint32_t *out,
                               uint32_t buffer_len);

/**
 * Copies result activation values into `out`.
 *
 * Writes at most `min(buffer_len, sproink_results_len(results))` elements
 * and returns the count.
 *
 * # Safety
 *
 * - `results` must be a valid pointer from `sproink_activate()`.
 * - `out` must point to a buffer of at least `buffer_len` `f64` elements, or
 *   be null (in which case this function is a no-op returning 0).
 */
uint32_t sproink_results_activations(const struct SproinkResults *results,
                                     double *out,
                                     uint32_t buffer_len);

/**
 * Copies result hop distances into `out`.
 *
 * Writes at most `min(buffer_len, sproink_results_len(results))` elements
 * and returns the count.
 *
 * # Safety
 *
 * - `results` must be a valid pointer from `sproink_activate()`.
 * - `out` must point to a buffer of at least `buffer_len` `u32` elements, or
 *   be null (in which case this function is a no-op returning 0).
 */
uint32_t sproink_results_distances(const struct SproinkResults *results,
                                   uint32_t *out,
                                   uint32_t buffer_len);

/**
 * Frees results previously returned by [`sproink_activate()`].
 *
 * # Safety
 *
 * - `results` must be a pointer returned by `sproink_activate()`, or null.
 * - Must not be called more than once on the same pointer.
 *   Passing a previously-freed pointer is undefined behavior.
 */
void sproink_results_free(struct SproinkResults *results);

/**
 * Extracts co-activation pairs from results.
 *
 * Returns a heap-allocated `SproinkPairs` pointer, or null on failure.
 * Free with [`sproink_pairs_free()`].
 *
 * # Safety
 *
 * - `results` must be a valid pointer from `sproink_activate()`.
 * - `seed_nodes` must point to an array of at least `num_seeds` elements,
 *   or be null when `num_seeds == 0`.
 */
struct SproinkPairs *sproink_extract_pairs(const struct SproinkResults *results,
                                           uint32_t num_seeds,
                                           const uint32_t *seed_nodes,
                                           double activation_threshold);

/**
 * Returns the number of co-activation pairs, or 0 if `pairs` is null.
 *
 * # Safety
 *
 * - `pairs` must be a valid pointer from `sproink_extract_pairs()`, or null.
 */
uint32_t sproink_pairs_len(const struct SproinkPairs *pairs);

/**
 * Copies pair node IDs into `out_a` and `out_b`.
 *
 * Writes at most `min(buffer_len, sproink_pairs_len(pairs))` elements to
 * each output buffer and returns the count.
 *
 * # Safety
 *
 * - `pairs` must be a valid pointer from `sproink_extract_pairs()`.
 * - `out_a` and `out_b` must each point to buffers of at least `buffer_len`
 *   `u32` elements, or be null (in which case this function is a no-op
 *   returning 0).
 */
uint32_t sproink_pairs_nodes(const struct SproinkPairs *pairs,
                             uint32_t *out_a,
                             uint32_t *out_b,
                             uint32_t buffer_len);

/**
 * Copies pair activation values into `out_a` and `out_b`.
 *
 * Writes at most `min(buffer_len, sproink_pairs_len(pairs))` elements to
 * each output buffer and returns the count.
 *
 * # Safety
 *
 * - `pairs` must be a valid pointer from `sproink_extract_pairs()`.
 * - `out_a` and `out_b` must each point to buffers of at least `buffer_len`
 *   `f64` elements, or be null (in which case this function is a no-op
 *   returning 0).
 */
uint32_t sproink_pairs_activations(const struct SproinkPairs *pairs,
                                   double *out_a,
                                   double *out_b,
                                   uint32_t buffer_len);

/**
 * Frees pairs previously returned by [`sproink_extract_pairs()`].
 *
 * # Safety
 *
 * - `pairs` must be a pointer returned by `sproink_extract_pairs()`, or null.
 * - Must not be called more than once on the same pointer.
 *   Passing a previously-freed pointer is undefined behavior.
 */
void sproink_pairs_free(struct SproinkPairs *pairs);

/**
 * Computes a single Oja weight update.
 *
 * Returns the updated weight, or `min_weight` on internal failure or invalid
 * inputs. Inputs are rejected (and `min_weight` returned) if any of:
 *
 * - `current_weight`, `activation_a`, `activation_b`, `learning_rate`,
 *   `min_weight`, or `max_weight` is NaN or infinite
 * - `learning_rate < 0.0`
 * - `min_weight < 0.0` or `max_weight > 1.0`
 * - `min_weight > max_weight`
 *
 * # Safety
 *
 * This function has no pointer parameters and is always safe to call.
 * It is marked `unsafe extern "C"` only for FFI compatibility.
 */
double sproink_oja_update(double current_weight,
                          double activation_a,
                          double activation_b,
                          double learning_rate,
                          double min_weight,
                          double max_weight);

#endif  /* SPROINK_H */
