package mssql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// estimatedPlan is what the server answers when it is asked what it would do.
// The operators an operator reads from are nested inside the element named
// after what it physically does, not inside the operator itself.
const estimatedPlan = `<?xml version="1.0"?>
<ShowPlanXML xmlns="http://schemas.microsoft.com/sqlserver/2004/07/showplan" Version="1.539">
 <BatchSequence><Batch><Statements>
  <StmtSimple StatementText="SELECT * FROM users u JOIN teams t ON t.id = u.team_id"
              StatementType="SELECT" StatementSubTreeCost="0.0328311">
   <QueryPlan CachedPlanSize="24">
    <RelOp NodeId="0" PhysicalOp="Nested Loops" LogicalOp="Inner Join"
           EstimateRows="1000" EstimatedTotalSubtreeCost="0.0328311">
     <NestedLoops Optimized="0">
      <RelOp NodeId="1" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan"
             EstimateRows="1000" EstimatedTotalSubtreeCost="0.0180425">
       <IndexScan Ordered="0">
        <Object Database="[app]" Schema="[dbo]" Table="[users]" Index="[PK_users]"/>
       </IndexScan>
      </RelOp>
      <RelOp NodeId="2" PhysicalOp="Index Seek" LogicalOp="Index Seek"
             EstimateRows="1" EstimatedTotalSubtreeCost="0.0032831">
       <IndexScan Ordered="1">
        <Object Database="[app]" Schema="[dbo]" Table="[teams]" Index="[PK_teams]"/>
       </IndexScan>
      </RelOp>
     </NestedLoops>
    </RelOp>
   </QueryPlan>
  </StmtSimple>
 </Statements></Batch></BatchSequence>
</ShowPlanXML>`

// actualPlan is what the server answers when it has run the statement, and it
// counts the rows once for every thread that produced any.
const actualPlan = `<ShowPlanXML xmlns="http://schemas.microsoft.com/sqlserver/2004/07/showplan">
 <BatchSequence><Batch><Statements>
  <StmtSimple StatementType="SELECT" StatementSubTreeCost="0.0032831">
   <QueryPlan>
    <RelOp NodeId="0" PhysicalOp="Table Scan" LogicalOp="Table Scan"
           EstimateRows="500" EstimatedTotalSubtreeCost="0.0032831">
     <RunTimeInformation>
      <RunTimeCountersPerThread Thread="1" ActualRows="400" ActualElapsedms="12"/>
      <RunTimeCountersPerThread Thread="2" ActualRows="342" ActualElapsedms="31"/>
     </RunTimeInformation>
     <TableScan Ordered="0">
      <Object Database="[app]" Schema="[dbo]" Table="[events]"/>
     </TableScan>
    </RelOp>
   </QueryPlan>
  </StmtSimple>
 </Statements></Batch></BatchSequence>
</ShowPlanXML>`

const plainPlan = `<ShowPlanXML><BatchSequence><Batch><Statements><StmtSimple><QueryPlan>
 <RelOp NodeId="0" PhysicalOp="Constant Scan" LogicalOp="Constant Scan"/>
</QueryPlan></StmtSimple></Statements></Batch></BatchSequence></ShowPlanXML>`

func TestParsePlanWalksThroughTheOperatorsWrapper(t *testing.T) {
	plan, err := ParsePlan(estimatedPlan)
	if err != nil {
		t.Fatalf("ParsePlan() = %v", err)
	}
	if plan.Root.Name != "Nested Loops" || plan.Total != 0.0328311 {
		t.Fatalf("root = %+v, total = %v", plan.Root, plan.Total)
	}
	if len(plan.Root.Children) != 2 {
		t.Fatalf("an operator reads from what is nested inside its wrapper: %+v", plan.Root)
	}
	scan := plan.Root.Children[0]
	if scan.Name != "Clustered Index Scan" || scan.Rows != 1000 || scan.Depth != 1 {
		t.Errorf("child = %+v", scan)
	}
	if !strings.Contains(scan.Detail, "dbo.users using PK_users") {
		t.Errorf("detail = %q", scan.Detail)
	}
	if plan.Root.Detail != "Inner Join" {
		t.Errorf("an operator whose two names differ says both: %q", plan.Root.Detail)
	}
	if plan.Root.Duration != 0 {
		t.Error("an estimate took no time, because nothing ran")
	}
	if !strings.Contains(plan.Text, "\n  Clustered Index Scan") {
		t.Errorf("text = %q", plan.Text)
	}
}

func TestParsePlanPrefersWhatReallyHappened(t *testing.T) {
	plan, err := ParsePlan(actualPlan)
	if err != nil {
		t.Fatalf("ParsePlan() = %v", err)
	}
	if plan.Root.Rows != 742 {
		t.Errorf("rows = %d, want every thread's rows counted", plan.Root.Rows)
	}
	if plan.Root.Duration != 31*time.Millisecond {
		t.Errorf("duration = %v, want the thread that took longest", plan.Root.Duration)
	}
	if !strings.Contains(plan.Root.Detail, "dbo.events") || strings.Contains(plan.Root.Detail, "using") {
		t.Errorf("detail = %q", plan.Root.Detail)
	}
}

func TestParsePlanOfAStatementWithNothingToPlan(t *testing.T) {
	if _, err := ParsePlan("<ShowPlanXML></ShowPlanXML>"); err == nil {
		t.Fatal("a plan with no operation must be an error")
	}
	if _, err := ParsePlan("<ShowPlanXML"); err == nil {
		t.Fatal("a document that is not a document must be an error")
	}
}

func TestParsePlanFallsBackToTheRootsCost(t *testing.T) {
	plan, err := ParsePlan(plainPlan)
	if err != nil {
		t.Fatalf("ParsePlan() = %v", err)
	}
	if plan.Root.Name != "Constant Scan" || plan.Root.Detail != "" {
		t.Errorf("root = %+v", plan.Root)
	}
	if plan.Total != 0 {
		t.Errorf("total = %v", plan.Total)
	}
}

func TestExplainAsksForAnEstimateWithoutRunningAnything(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectExec(showPlanOn).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT 1").WillReturnRows(
		sqlmock.NewRows([]string{"plan"}).AddRow(plainPlan))
	mock.ExpectExec(showPlanOff).WillReturnResult(sqlmock.NewResult(0, 0))

	plan, err := conn.Explain(context.Background(), "SELECT 1", false)
	if err != nil {
		t.Fatalf("Explain() = %v", err)
	}
	if plan.Root.Name != "Constant Scan" {
		t.Errorf("plan = %+v", plan)
	}
}

func TestExplainReadsTheTimedPlanAfterTheRows(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	rows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	plan := sqlmock.NewRows([]string{"plan"}).AddRow(actualPlan)
	mock.ExpectExec(statisticsOn).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT id FROM events").WillReturnRows(rows, plan)
	mock.ExpectExec(statisticsOff).WillReturnResult(sqlmock.NewResult(0, 0))

	got, err := conn.Explain(context.Background(), "SELECT id FROM events", true)
	if err != nil {
		t.Fatalf("Explain() = %v", err)
	}
	if got.Root.Rows != 742 {
		t.Errorf("plan = %+v", got.Root)
	}
}

func TestExplainReportsWhatWentWrong(t *testing.T) {
	t.Run("a server that will not explain", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectExec(showPlanOn).WillReturnError(errRefused)
		if _, err := conn.Explain(context.Background(), "SELECT 1", false); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("a statement that will not plan", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectExec(showPlanOn).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT nope").WillReturnError(errRefused)
		if _, err := conn.Explain(context.Background(), "SELECT nope", false); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("a statement the server has no plan for", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectExec(showPlanOn).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SET NOEXEC ON").WillReturnRows(sqlmock.NewRows([]string{"a"}).AddRow("no plan here"))
		if _, err := conn.Explain(context.Background(), "SET NOEXEC ON", false); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("a connection that cannot be put back the way it was found", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectExec(showPlanOn).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow(plainPlan))
		mock.ExpectExec(showPlanOff).WillReturnError(errRefused)
		if _, err := conn.Explain(context.Background(), "SELECT 1", false); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestParsePlanOfOperatorsThatSayLittle(t *testing.T) {
	document := `<ShowPlanXML><BatchSequence><Batch><Statements><StmtSimple StatementSubTreeCost="oops">
	 <QueryPlan>
	  <RelOp NodeId="0" EstimateRows="not a number" EstimatedTotalSubtreeCost="also not">
	   <Sort>
	    <Object Index="[IX_only]"/>
	    <RelOp NodeId="1" PhysicalOp="Table Scan" LogicalOp="Table Scan">
	     <TableScan><Object Schema="[dbo]" Table="[t]"/></TableScan>
	    </RelOp>
	   </Sort>
	  </RelOp>
	 </QueryPlan></StmtSimple></Statements></Batch></BatchSequence></ShowPlanXML>`
	plan, err := ParsePlan(document)
	if err != nil {
		t.Fatalf("ParsePlan() = %v", err)
	}
	if plan.Root.Name != "operation" {
		t.Errorf("an operator that does not say what it does is still an operation: %q", plan.Root.Name)
	}
	if plan.Root.Rows != 0 || plan.Root.Cost != 0 || plan.Total != 0 {
		t.Errorf("a number that is not a number is no measurement: %+v", plan.Root)
	}
	if plan.Root.Detail != "on IX_only" {
		t.Errorf("an operator that names only an index says only the index: %q", plan.Root.Detail)
	}
	if len(plan.Root.Children) != 1 || plan.Root.Children[0].Detail != "on dbo.t" {
		t.Errorf("children = %+v", plan.Root.Children)
	}
}

func TestOnlyTheFirstObjectAnOperatorNamesIsKept(t *testing.T) {
	document := `<ShowPlanXML><BatchSequence><Batch><Statements><StmtSimple><QueryPlan>
	 <RelOp PhysicalOp="Merge Join"><Merge>
	  <Object Schema="[dbo]" Table="[first]"/>
	  <Object Schema="[dbo]" Table="[second]"/>
	 </Merge></RelOp>
	</QueryPlan></StmtSimple></Statements></Batch></BatchSequence></ShowPlanXML>`
	plan, err := ParsePlan(document)
	if err != nil {
		t.Fatalf("ParsePlan() = %v", err)
	}
	if plan.Root.Detail != "on dbo.first" {
		t.Errorf("detail = %q", plan.Root.Detail)
	}
}

func TestExplainIgnoresAResultThatIsNotAPlan(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	wide := sqlmock.NewRows([]string{"a", "b"}).AddRow(int64(1), int64(2))
	plan := sqlmock.NewRows([]string{"plan"}).AddRow(plainPlan)
	mock.ExpectExec(statisticsOn).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT a, b FROM t").WillReturnRows(wide, plan)
	mock.ExpectExec(statisticsOff).WillReturnResult(sqlmock.NewResult(0, 0))

	got, err := conn.Explain(context.Background(), "SELECT a, b FROM t", true)
	if err != nil {
		t.Fatalf("Explain() = %v", err)
	}
	if got.Root.Name != "Constant Scan" {
		t.Errorf("plan = %+v", got.Root)
	}
}

func TestExplainOfAStatementThatReturnedNoRowsAtAll(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectExec(showPlanOn).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"plan"}))
	if _, err := conn.Explain(context.Background(), "SELECT 1", false); err == nil {
		t.Fatal("a plan that never arrived must be an error")
	}
}
