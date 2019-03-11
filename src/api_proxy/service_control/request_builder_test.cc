// Copyright 2018 Google Cloud Platform Proxy Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include "src/api_proxy/service_control/request_builder.h"
#include "gtest/gtest.h"

#include <assert.h>
#include <fstream>
#include <string>

#include "google/protobuf/struct.pb.h"
#include "google/protobuf/text_format.h"

#include "src/api_proxy/utils/version.h"

namespace gasv1 = ::google::api::servicecontrol::v1;
using ::google::protobuf::Timestamp;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_proxy {
namespace service_control {

namespace {

const char kFakeVersion[] = "TEST.0.0";
const char kTestdata[] = "src/api_proxy/service_control/testdata/";

std::string ReadTestBaseline(const char* input_file_name) {
  std::string file_name = std::string(kTestdata) + input_file_name;

  std::string contents;
  std::ifstream input_file;
  input_file.open(file_name, std::ifstream::in | std::ifstream::binary);
  EXPECT_TRUE(input_file.is_open()) << file_name;
  input_file.seekg(0, std::ios::end);
  contents.reserve(input_file.tellg());
  input_file.seekg(0, std::ios::beg);
  contents.assign((std::istreambuf_iterator<char>(input_file)),
                  (std::istreambuf_iterator<char>()));

  // Replace instances of {{service_agent_version}} with the expected service
  // agent version.
  std::string placeholder = "{{service_agent_version}}";
  size_t current = 0;
  while ((current = contents.find(placeholder, current)) != std::string::npos) {
    contents.replace(current, placeholder.length(), kFakeVersion);
    current += strlen(kFakeVersion);
  }
  return contents;
}

void FillOperationInfo(OperationInfo* op) {
  op->operation_id = "operation_id";
  op->operation_name = "operation_name";
  op->api_key = "api_key_x";
  op->producer_project_id = "project_id";
}

void FillCheckRequestInfo(CheckRequestInfo* request) {
  request->client_ip = "1.2.3.4";
  request->referer = "referer";
}

void FillCheckRequestAndroidInfo(CheckRequestInfo* request) {
  request->android_package_name = "com.google.cloud";
  request->android_cert_fingerprint = "ABCDESF";
  request->ios_bundle_id = "5b40ad6af9a806305a0a56d7cb91b82a27c26909";
}

void FillAllocateQuotaRequestInfo(QuotaRequestInfo* request) {
  request->client_ip = "1.2.3.4";
  request->referer = "referer";
  request->method_name = "operation_name";
}

void FillReportRequestInfo(ReportRequestInfo* request) {
  request->referer = "referer";
  request->response_code = 200;
  request->location = "us-central";
  request->api_name = "api-name";
  request->api_version = "api-version";
  request->api_method = "api-method";
  request->request_size = 100;
  request->response_size = 1024 * 1024;
  request->log_message = "test-method is called";
  request->latency.request_time_ms = 123;
  request->latency.backend_time_ms = 101;
  request->latency.overhead_time_ms = 22;
  request->frontend_protocol = protocol::HTTP;
  request->compute_platform = compute_platform::GKE;
  request->auth_issuer = "auth-issuer";
  request->auth_audience = "auth-audience";

  request->request_bytes = 100;
  request->response_bytes = 1024 * 1024;
}

void SetFixTimeStamps(gasv1::Operation* op) {
  Timestamp fix_time;
  fix_time.set_seconds(100000);
  fix_time.set_nanos(100000);
  *op->mutable_start_time() = fix_time;
  *op->mutable_end_time() = fix_time;
  if (op->log_entries().size() > 0) {
    *op->mutable_log_entries(0)->mutable_timestamp() = fix_time;
    op->mutable_log_entries(0)
        ->mutable_struct_payload()
        ->mutable_fields()
        ->erase("timestamp");
  }
}

std::string CheckRequestToString(gasv1::CheckRequest* request) {
  gasv1::Operation* op = request->mutable_operation();
  SetFixTimeStamps(op);

  std::string text;
  google::protobuf::TextFormat::PrintToString(*request, &text);
  return text;
}

std::string AllocateQuotaRequestToString(gasv1::AllocateQuotaRequest* request) {
  std::string text;
  google::protobuf::TextFormat::PrintToString(*request, &text);
  return text;
}

std::string ReportRequestToString(gasv1::ReportRequest* request) {
  for (int i = 0; i < request->operations_size(); i++) {
    SetFixTimeStamps(request->mutable_operations(i));
  }

  std::string text;
  google::protobuf::TextFormat::PrintToString(*request, &text);
  return text;
}

class RequestBuilderTest : public ::testing::Test {
 protected:
  static void SetUpTestCase() {
    // Inject the fake version in the singleton version instance.
    utils::Version::instance().set(kFakeVersion);
  }

  RequestBuilderTest()
      : scp_({"local_test_log"}, "test_service", "2016-09-19r0") {}

  RequestBuilder scp_;
};

TEST(RequestBuilder, TestRequestBuilderbufStruct) {
  // Verify if ::google::protobuf::Struct works.
  // If the main binary code is compiled with CXXFLAGS=-std=c++11,
  // and protobuf library is not, ::google::protobuf::Struct will crash.
  ::google::protobuf::Struct st;
  auto* fields = st.mutable_fields();
  (*fields)["test"].set_string_value("value");
  ASSERT_FALSE(fields->empty());
}

TEST_F(RequestBuilderTest, FillGoodCheckRequestTest) {
  CheckRequestInfo info;
  FillOperationInfo(&info);
  FillCheckRequestInfo(&info);

  gasv1::CheckRequest request;
  ASSERT_TRUE(scp_.FillCheckRequest(info, &request).ok());

  std::string text = CheckRequestToString(&request);
  std::string expected_text = ReadTestBaseline("check_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillGoodCheckRequestAndroidIosTest) {
  CheckRequestInfo info;
  FillOperationInfo(&info);
  FillCheckRequestInfo(&info);
  FillCheckRequestAndroidInfo(&info);

  gasv1::CheckRequest request;
  ASSERT_TRUE(scp_.FillCheckRequest(info, &request).ok());

  std::string text = CheckRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("check_request_android_ios.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillGoodAllocateQuotaRequestTest) {
  std::vector<std::pair<std::string, int>> metric_cost_vector = {
      {"metric_first", 1}, {"metric_second", 2}};

  QuotaRequestInfo info;
  info.metric_cost_vector = &metric_cost_vector;

  FillOperationInfo(&info);
  FillAllocateQuotaRequestInfo(&info);

  gasv1::AllocateQuotaRequest request;
  ASSERT_TRUE(scp_.FillAllocateQuotaRequest(info, &request).ok());

  std::string text = AllocateQuotaRequestToString(&request);
  std::string expected_text = ReadTestBaseline("allocate_quota_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillAllocateQuotaRequestNoMethodNameTest) {
  std::vector<std::pair<std::string, int>> metric_cost_vector = {
      {"metric_first", 1}, {"metric_second", 2}};

  QuotaRequestInfo info;
  FillOperationInfo(&info);
  info.metric_cost_vector = &metric_cost_vector;
  info.client_ip = "1.2.3.4";
  info.referer = "referer";
  info.method_name = "";

  gasv1::AllocateQuotaRequest request;
  ASSERT_TRUE(scp_.FillAllocateQuotaRequest(info, &request).ok());

  std::string text = AllocateQuotaRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("allocate_quota_request_no_method_name.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillNoApiKeyCheckRequestTest) {
  CheckRequestInfo info;
  info.operation_id = "operation_id";
  info.operation_name = "operation_name";
  info.producer_project_id = "project_id";

  gasv1::CheckRequest request;
  ASSERT_TRUE(scp_.FillCheckRequest(info, &request).ok());

  std::string text = CheckRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("check_request_no_api_key.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, CheckRequestMissingOperationNameTest) {
  CheckRequestInfo info;
  info.operation_id = "operation_id";

  gasv1::CheckRequest request;
  ASSERT_EQ(Code::INVALID_ARGUMENT,
            scp_.FillCheckRequest(info, &request).error_code());
}

TEST_F(RequestBuilderTest, CheckRequestMissingOperationIdTest) {
  CheckRequestInfo info;
  info.operation_name = "operation_name";

  gasv1::CheckRequest request;
  ASSERT_EQ(Code::INVALID_ARGUMENT,
            scp_.FillCheckRequest(info, &request).error_code());
}

TEST_F(RequestBuilderTest, FillGoodReportRequestTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);
  info.backend_protocol = protocol::GRPC;

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillGoodReportRequestByConsumerTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);
  info.backend_protocol = protocol::GRPC;
  info.check_response_info.consumer_project_id =
      ::google::protobuf::StringPiece("12345");

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("report_request_by_consumer.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillStartReportRequestTest) {
  ReportRequestInfo info;
  info.is_first_report = true;
  info.is_final_report = false;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("first_report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillIntermediateReportRequestTest) {
  ReportRequestInfo info;
  info.is_first_report = false;
  info.is_final_report = false;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("intermediate_report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillFinalReportRequestTest) {
  ReportRequestInfo info;
  info.is_first_report = false;
  info.is_final_report = true;
  FillOperationInfo(&info);
  FillReportRequestInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("final_report_request.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillReportRequestFailedTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  // Remove api_key to test not api_key case for
  // producer_project_id and credential_id.
  info.api_key = "";
  FillReportRequestInfo(&info);

  // Use 401 as a failed response code.
  info.response_code = 401;

  // Use the corresponding status for that response code.
  info.status = Status(Code::PERMISSION_DENIED, "");

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text = ReadTestBaseline("report_request_failed.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, FillReportRequestEmptyOptionalTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  std::string text = ReportRequestToString(&request);
  std::string expected_text =
      ReadTestBaseline("report_request_empty_optional.golden");
  ASSERT_EQ(expected_text, text);
}

TEST_F(RequestBuilderTest, CredentailIdApiKeyTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  ASSERT_EQ(request.operations(0).labels().at("/credential_id"),
            "apikey:api_key_x");
}

TEST_F(RequestBuilderTest, CredentailIdIssuerOnlyTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  info.api_key = "";
  info.auth_issuer = "auth-issuer";

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  // TODO: (qiwzhang) credentail_id for auth is disabled for now
  //  ASSERT_EQ(request.operations(0).labels().at("/credential_id"),
  //            "jwtauth:issuer=YXV0aC1pc3N1ZXI");
}

TEST_F(RequestBuilderTest, CredentailIdIssuerAudienceTest) {
  ReportRequestInfo info;
  FillOperationInfo(&info);
  info.api_key = "";
  info.auth_issuer = "auth-issuer";
  info.auth_audience = "auth-audience";

  gasv1::ReportRequest request;
  ASSERT_TRUE(scp_.FillReportRequest(info, &request).ok());

  // TODO: (qiwzhang) credentail_id for auth is disabled for now
  // ASSERT_EQ(request.operations(0).labels().at("/credential_id"),
  //          "jwtauth:issuer=YXV0aC1pc3N1ZXI&audience=YXV0aC1hdWRpZW5jZQ");
}

}  // namespace

}  // namespace service_control
}  // namespace api_proxy
}  // namespace google
