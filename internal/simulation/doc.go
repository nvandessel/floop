// Package simulation provides a multi-session test harness for validating
// emergent dynamics of the activation pipeline.
//
// The simulation exercises the real Engine, OjaUpdate, SQLiteGraphStore, and
// tiering pipeline — no mocks. Scenarios are Go builders that construct
// pre-seeded graphs and run configurable numbers of activation sessions,
// capturing edge weight snapshots for property-based assertions.
//
// Each test gets an isolated SQLite database via t.TempDir() and a sandboxed
// HOME to prevent touching user data.
//
// Usage:
//
//	func TestOjaConvergence(t *testing.T) {
//	    r := simulation.NewRunner(t)
//	    result := r.Run(simulation.Scenario{
//	        Name:           "oja-convergence",
//	        Behaviors:      []simulation.BehaviorSpec{...},
//	        Edges:          []simulation.EdgeSpec{...},
//	        Sessions:       []simulation.SessionContext{...},
//	        HebbianEnabled: true,
//	    })
//	    simulation.AssertWeightConverges(t, result, "a", "b", "co-activated", 0.85, 0.95, 40)
//	}
package simulation
