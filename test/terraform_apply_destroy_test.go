package test

import (
	"os"
	"strings"
	"testing"
	"time"

	"context"

	"github.com/gruntwork-io/terratest/modules/azure"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
	"github.com/sethvargo/go-password/password"
	"github.com/stretchr/testify/assert"
)

func getDefaultTerraformOptions(t *testing.T) (string, *terraform.Options, error) {

	tempTestFolder := test_structure.CopyTerraformFolderToTemp(t, "..", ".")

	sqlServerAdmin := random.UniqueId()
	sqlServerPass, err := password.Generate(30, 10, 10, false, true)
	if err != nil {
		return "", nil, err
	}

	dataLakeName := "tfadlt" + strings.ToLower(random.UniqueId())

	region, err := azure.GetRandomRegionE(t, []string{
		"centralus",
		"eastus",
		"eastus2",
		"westus",
		"westus2",
		"southcentralus",
		"northeurope",
		"westeurope",
		"francecentral",
		"uksouth",
	}, nil, "")
	if err != nil {
		return "", nil, err
	}

	terraformOptions := &terraform.Options{
		TerraformDir: tempTestFolder,
		Vars:         map[string]interface{}{},
		RetryableTerraformErrors: map[string]string{
			".*429.*": "Failed to create notebooks due to rate limiting",
			".*does not have any associated worker environments.*:":        "Databricks API was not ready for requests",
			".*we are currently experiencing high demand in this region.*": "Azure service at capacity",
			".*connection reset by peer.*":                                 "Temporary connectivity issue",
			".*Error 403 Failed to retrieve tenant ID for given token.*":   "Databricks access token not valid yet",
			".*Timeout exceeded while awaiting headers.*":                  "Databricks HTTP timeout",
		},
		MaxRetries:         5,
		TimeBetweenRetries: 5 * time.Minute,
		NoColor:            true,
		Logger:             logger.TestingT,
	}

	terraformOptions.Vars["data_lake_name"] = dataLakeName
	terraformOptions.Vars["sql_server_admin_username"] = sqlServerAdmin
	terraformOptions.Vars["sql_server_admin_password"] = sqlServerPass
	terraformOptions.Vars["region"] = region

	// used to distinguish resources from each test run
	githubRunID, inGithub := os.LookupEnv("GITHUB_RUN_ID")
	if inGithub {
		terraformOptions.Vars["extra_tags"] = map[string]string{
			"Terratest": githubRunID,
		}
	}

	// default values
	terraformOptions.Vars["storage_replication"] = "LRS"
	terraformOptions.Vars["service_principal_end_date"] = time.Now().Add(time.Hour * 4).Format(time.RFC3339)
	terraformOptions.Vars["data_warehouse_dtu"] = "DW100c"
	terraformOptions.Vars["cosmosdb_consistency_level"] = "Session"

	return dataLakeName, terraformOptions, nil
}

func TestApplyAndDestroyWithSamples(t *testing.T) {
	t.Parallel()

	name, options, err := getDefaultTerraformOptions(t)
	assert.NoError(t, err)

	defer terraform.Destroy(t, options)
	_, err = terraform.InitAndApplyE(t, options)
	assert.NoError(t, err)

	outDataLakeName, err := terraform.OutputE(t, options, "name")
	assert.NoError(t, err)

	assert.Equal(t, name, outDataLakeName)
}

func TestApplyAndDestroyWithoutSamples(t *testing.T) {
	t.Parallel()

	name, options, err := getDefaultTerraformOptions(t)
	assert.NoError(t, err)

	options.Vars["provision_sample_data"] = false

	defer terraform.Destroy(t, options)
	_, err = terraform.InitAndApplyE(t, options)
	assert.NoError(t, err)

	outDataLakeName, err := terraform.OutputE(t, options, "name")
	assert.NoError(t, err)

	assert.Equal(t, name, outDataLakeName)
}

func TestApplyAndDestroyWithoutSynapse(t *testing.T) {
	t.Parallel()

	name, options, err := getDefaultTerraformOptions(t)
	assert.NoError(t, err)

	options.Vars["provision_synapse"] = false
	delete(options.Vars, "data_warehouse_dtu")
	delete(options.Vars, "sql_server_admin_username")
	delete(options.Vars, "sql_server_admin_password")

	defer terraform.Destroy(t, options)
	_, err = terraform.InitAndApplyE(t, options)
	assert.NoError(t, err)

	outDataLakeName, err := terraform.OutputE(t, options, "name")
	assert.NoError(t, err)

	assert.Equal(t, name, outDataLakeName)
}

func TestApplyAndDestroyWithKeyVault(t *testing.T) {
	t.Parallel()

	subscriptionID, exists := os.LookupEnv("ARM_SUBSCRIPTION_ID")
	assert.True(t, exists)

	clientID, exists := os.LookupEnv("ARM_CLIENT_ID")
	assert.True(t, exists)

	clientSecret, exists := os.LookupEnv("ARM_CLIENT_SECRET")
	assert.True(t, exists)

	tenantID, exists := os.LookupEnv("ARM_TENANT_ID")
	assert.True(t, exists)

	name, options, err := getDefaultTerraformOptions(t)
	assert.NoError(t, err)

	ctx := context.Background()

	kvName := "kv" + name
	rgName := "rgkv" + name

	kv, err := setupKeyVault(ctx, kvName, rgName, options.Vars["region"].(string), subscriptionID, clientID, clientSecret, tenantID)
	assert.NoError(t, err)

	defer func() {
		_, err = destroyResourceGroup(ctx, rgName, subscriptionID, clientID, clientSecret, tenantID)
		assert.NoError(t, err)
	}()

	options.Vars["provision_sample_data"] = false
	options.Vars["use_key_vault"] = true
	options.Vars["key_vault_id"] = *kv.ID

	defer terraform.Destroy(t, options)
	_, err = terraform.InitAndApplyE(t, options)
	assert.NoError(t, err)

	outDataLakeName, err := terraform.OutputE(t, options, "name")
	assert.NoError(t, err)

	assert.Equal(t, name, outDataLakeName)
}

func TestApplyAndDestroyWithoutDatabricks(t *testing.T) {
	t.Parallel()

	name, options, err := getDefaultTerraformOptions(t)
	assert.NoError(t, err)

	options.Vars["provision_databricks"] = false

	defer terraform.Destroy(t, options)
	_, err = terraform.InitAndApplyE(t, options)
	assert.NoError(t, err)

	outDataLakeName, err := terraform.OutputE(t, options, "name")
	assert.NoError(t, err)

	assert.Equal(t, name, outDataLakeName)
}
