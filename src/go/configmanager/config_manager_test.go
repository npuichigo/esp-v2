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

package configmanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloudesf.googlesource.com/gcpproxy/src/go/configmanager/testdata"
	"cloudesf.googlesource.com/gcpproxy/src/go/flags"
	"cloudesf.googlesource.com/gcpproxy/src/go/util"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"google.golang.org/genproto/protobuf/api"

	ut "cloudesf.googlesource.com/gcpproxy/src/go/util"
	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	conf "google.golang.org/genproto/googleapis/api/serviceconfig"
)

const (
	testProjectName  = "bookstore.endpoints.project123.cloud.goog"
	testEndpointName = "endpoints.examples.bookstore.Bookstore"
	testConfigID     = "2017-05-01r0"
	testProjectID    = "project123"
	fakeNodeID       = "id"
	fakeJwks         = "FAKEJWKS"
)

var (
	fakeConfig          = ``
	fakeRollout         = ``
	fakeProtoDescriptor = base64.StdEncoding.EncodeToString([]byte("rawDescriptor"))
)

func TestFetchListeners(t *testing.T) {
	testData := []struct {
		desc              string
		backendProtocol   string
		fakeServiceConfig string
		wantedListeners   string
	}{
		{
			desc:            "Success for gRPC backend with transcoding",
			backendProtocol: "gRPC",
			fakeServiceConfig: fmt.Sprintf(`{
				"name":"%s",
				"apis":[
					{
						"name":"%s",
						"version":"v1",
						"syntax":"SYNTAX_PROTO3"
					}
				],
				"sourceInfo":{
					"sourceFiles":[
						{
							"@type":"type.googleapis.com/google.api.servicemanagement.v1.ConfigFile",
							"filePath":"api_descriptor.pb",
							"fileContents":"%s",
							"fileType":"FILE_DESCRIPTOR_SET_PROTO"
						}
					]
				}
			}`, testProjectName, testEndpointName, fakeProtoDescriptor),
			wantedListeners: fmt.Sprintf(`{
				"address":{
					"socketAddress":{
						"address":"0.0.0.0",
						"portValue":8080
					}
				},
				"filterChains":[
					{
						"filters":[
							{
								"config":{
									"http_filters":[
										{
											"config":{
												"ignored_query_parameters": [
                                                    "api_key",
													"key"
												],
												"proto_descriptor_bin":"%s",
												"services":[
													"%s"
												]
											},
											"name":"envoy.grpc_json_transcoder"
										},
										{
											"config":{
											},
											"name":"envoy.grpc_web"
										},
										{
											"config":{
											},
											"name":"envoy.router"
										}
									],
									"route_config":{
										"name":"local_route",
										"virtual_hosts":[
											{
												"domains":[
													"*"
												],
												"name":"backend",
												"routes":[
													{
														"match":{
															"prefix":"/"
														},
														"route":{
															"cluster": "%s"
														}
													}
												]
											}
										]
									},
									"stat_prefix":"ingress_http"
								},
								"name":"envoy.http_connection_manager"
							}
						]
					}
				]
			}`,
				fakeProtoDescriptor, testEndpointName, testEndpointName),
		},
		{
			desc:            "Success for gRPC backend, with Jwt filter, with audiences, no Http Rules",
			backendProtocol: "grpc",
			fakeServiceConfig: fmt.Sprintf(`{
				"apis":[
					{
						"name":"%s"
					}
				],
				"authentication": {
					"providers": [
						{
							"id": "firebase",
							"issuer": "https://test_issuer.google.com/",
							"jwks_uri": "$JWKSURI",
							"audiences": "test_audience1, test_audience2 "
						},
                        {
                            "id": "unknownId",
                            "issuer": "https://test_issuer.google.com/",
                            "jwks_uri": "invalidUrl"
                        }
					],
					"rules": [
                        {
                	        "selector": "endpoints.examples.bookstore.Bookstore.CreateShelf",
                            "requirements": [
                                {
                                    "provider_id": "firebase",
                                    "audiences": "test_audience1"
                                }
                            ]
                        },
                        {
                	        "selector": "endpoints.examples.bookstore.Bookstore.ListShelves"
                        }
        	        ]
                }
            }`, testEndpointName),
			wantedListeners: fmt.Sprintf(`{
                "filters":[
                    {
                        "config":{
                            "http_filters":[
                                {
                                    "config": {
                                        "providers": {
                                            "firebase": {
                                                "audiences":["test_audience1", "test_audience2"],
                                               	"issuer":"https://test_issuer.google.com/",
                                               	"local_jwks": {
                                               	    "inline_string": "%s"
                                               	}
                                            }
                                        },
                                        "rules": [
                                            {
                                                "match":{
                                                    "path":"/endpoints.examples.bookstore.Bookstore/CreateShelf"
                                                },
                                                "requires": {
                                                    "provider_and_audiences": {
                                                	"audiences": ["test_audience1"],
                                                        "provider_name":"firebase"
                                                    }
                                                }
                                            }
					                    ]
                                    },
                                    "name":"envoy.filters.http.jwt_authn"
                                },
								{
									"config":{
									},
									"name":"envoy.grpc_web"
								},
                                {
                                    "config":{
                                    },
                                    "name":"envoy.router"
                                 }
                            ],
                            "route_config":{
                                "name":"local_route",
                                "virtual_hosts":[
                                    {
                                        "domains":[
                                            "*"
                                        ],
                                        "name":"backend",
                                            "routes":[
                                                {
                                                    "match":{
                                                        "prefix":"/"
                                                    },
                                                    "route":{
                                                        "cluster": "%s"
                                                    }
                                                }
                                            ]
                                        }
                                    ]
                                },
                            "stat_prefix":"ingress_http"
                         },
                        "name":"envoy.http_connection_manager"
                    }
                ]
            }`, fakeJwks, testEndpointName),
		},
		{
			desc:            "Success for gRPC backend, with Jwt filter, without audiences",
			backendProtocol: "gRPC",
			fakeServiceConfig: fmt.Sprintf(`{
                "apis":[
                    {
                        "name":"%s"
                    }
                ],
                "http": {
                    "rules": [
                        {
                            "selector": "endpoints.examples.bookstore.Bookstore.ListShelves",
                            "get": "/v1/shelves"
                        },
                        {
                            "selector": "endpoints.examples.bookstore.Bookstore.CreateShelf",
                            "post": "/v1/shelves/{shelf}"
                        }
                    ]
                },
                "authentication": {
        	        "providers": [
        	            {
        	 	            "id": "firebase",
        	 	            "issuer": "https://test_issuer.google.com/",
        	 	            "jwks_uri": "$JWKSURI"
        	            }
        	        ],
        	        "rules": [
                        {
                	        "selector": "endpoints.examples.bookstore.Bookstore.CreateShelf",
                            "requirements": [
                                {
                                    "provider_id": "firebase"
                                }
                            ]
                        },
                        {
                            "selector": "endpoints.examples.bookstore.Bookstore.ListShelves",
                            "requirements": [
                                {
                                    "provider_id": "firebase"
                                }
                            ]
                        }
        	        ]
                }
            }`, testEndpointName),
			wantedListeners: fmt.Sprintf(`{
                "filters":[
                    {
                        "config":{
                            "http_filters":[
                                {
                                    "config": {
                                        "providers": {
                                            "firebase": {
                                               	"issuer":"https://test_issuer.google.com/",
                                               	"local_jwks": {
                                               	    "inline_string": "%s"
                                               	}
                                            }
                                        },
                                        "rules": [
                                            {
                                               "match":{
                                                   "headers": [
                                                       {
                                                           "exact_match": "POST",
                                                           "name" : ":method"
                                                       }
                                                   ],
                                                   "regex": "/v1/shelves/[^\\/]+$"
                                                },
                                                "requires":{
                                                    "provider_name":"firebase"
                                                }
                                            },
					                        {
                                                "match":{
                                                    "path":"/endpoints.examples.bookstore.Bookstore/CreateShelf"
                                                },
                                                "requires": {
                                                    "provider_name":"firebase"
                                                }
                                            },
                                            {
                                                "match":{
                                                   "headers": [
                                                       {
                                                           "exact_match": "GET",
                                                           "name" : ":method"
                                                       }
                                                   ],
                                                   "path": "/v1/shelves"
                                                },
                                                "requires":{
                                                    "provider_name":"firebase"
                                                }
                                            },
					                        {
                                                "match":{
                                                    "path":"/endpoints.examples.bookstore.Bookstore/ListShelves"
                                                },
                                                "requires": {
                                                    "provider_name":"firebase"
                                                }
                                            }
                                        ]
                                    },
                                    "name":"envoy.filters.http.jwt_authn"
                                },
                                {
                                    "config":{
                                    },
                                    "name":"envoy.grpc_web"
                                },
                                {
                                    "config":{
                                    },
                                    "name":"envoy.router"
                                }
                            ],
                            "route_config":{
                                "name":"local_route",
                                "virtual_hosts":[
                                    {
                                        "domains":[
                                            "*"
                                        ],
                                        "name":"backend",
                                            "routes":[
                                                {
                                                    "match":{
                                                        "prefix":"/"
                                                    },
                                                    "route":{
                                                        "cluster": "%s"
                                                    }
                                                }
                                            ]
                                        }
                                    ]
                                },
                            "stat_prefix":"ingress_http"
                         },
                        "name":"envoy.http_connection_manager"
                    }
                ]
            }`, fakeJwks, testEndpointName),
		},
		{
			desc: "Success for gRPC backend, with Jwt filter, with multi requirements, matching with regex", backendProtocol: "gRPC",
			fakeServiceConfig: fmt.Sprintf(`{
                "apis":[
                    {
                        "name":"%s",
                        "sourceContext": {
							"fileName": "bookstore.proto"
						}
					}
                ],
                "http": {
                    "rules": [
                        {
                            "selector": "endpoints.examples.bookstore.Bookstore.GetBook",
                            "get": "/v1/shelves/{shelf}/books/{book}"
                        },
                        {
                            "selector": "endpoints.examples.bookstore.Bookstore.DeleteBook",
                            "delete": "/v1/shelves/{shelf}/books/{book}"
                        }
                    ]
                },
                "authentication": {
        	        "providers": [
        	            {
        	 	            "id": "firebase1",
        	 	            "issuer": "https://test_issuer.google.com/",
        	 	            "jwks_uri": "$JWKSURI"
        	            },
         	            {
        	 	            "id": "firebase2",
        	 	            "issuer": "https://test_issuer.google.com/",
        	 	            "jwks_uri": "$JWKSURI"
        	            }
        	        ],
        	        "rules": [
                        {
                            "selector": "endpoints.examples.bookstore.Bookstore.GetBook",
                            "requirements": [
                                {
                                    "provider_id": "firebase1"
                                },
                                {
                                    "provider_id": "firebase2"
                                }
                            ]
                        }
        	        ]
                }
            }`, testEndpointName),
			wantedListeners: fmt.Sprintf(`{
                "filters":[
                    {
                        "config":{
                            "http_filters":[
                                {
                                    "config": {
                                        "providers": {
                                            "firebase1": {
                                               	"issuer":"https://test_issuer.google.com/",
                                               	"local_jwks": {
                                               	    "inline_string": "%s"
                                               	}
                                            },
                                            "firebase2": {
                                               	"issuer":"https://test_issuer.google.com/",
                                               	"local_jwks": {
                                               	    "inline_string": "%s"
                                               	}
                                            }
                                        },
                                        "rules": [
                                            {
                                                "match":{
                                                    "headers": [
                                                        {
                                                            "exact_match": "GET",
                                                            "name" : ":method"
                                                        }
                                                    ],
                                                    "regex": "/v1/shelves/[^\\/]+/books/[^\\/]+$"
                                                },
						                        "requires": {
                                                    "requires_any": {
                                                    	"requirements": [
                                                    	    {
                                                    	    	"provider_name": "firebase1"
                                                    	    },
                                                    	    {
                                                    	    	"provider_name": "firebase2"
                                                    	    }
                                                    	]
                                                    }
					                            }
                                            },
					                        {
                                                "match":{
                                                    "path":"/endpoints.examples.bookstore.Bookstore/GetBook"
                                                },
                                                "requires": {
                                                    "requires_any": {
                                                    	"requirements": [
                                                    	    {
                                                    	    	"provider_name": "firebase1"
                                                    	    },
                                                    	    {
                                                    	    	"provider_name": "firebase2"
                                                    	    }
                                                    	]
                                                    }
                                                }
                                            }
                                        ]
                                    },
                                    "name":"envoy.filters.http.jwt_authn"
                                },
                                {
                                    "config":{
                                    },
                                    "name":"envoy.grpc_web"
                                },
                                {
                                    "config":{
                                    },
                                    "name":"envoy.router"
                                }
                            ],
                            "route_config":{
                                "name":"local_route",
                                "virtual_hosts":[
                                    {
                                        "domains":[
                                            "*"
                                        ],
                                        "name":"backend",
                                            "routes":[
                                                {
                                                    "match":{
                                                        "prefix":"/"
                                                    },
                                                    "route":{
                                                        "cluster": "%s"
                                                    }
                                                }
                                            ]
                                        }
                                    ]
                                },
                            "stat_prefix":"ingress_http"
                         },
                        "name":"envoy.http_connection_manager"
                    }
                ]
            }`, fakeJwks, fakeJwks, testEndpointName),
		},
		{
			desc:            "Success for gRPC backend with Service Control",
			backendProtocol: "gRPC",
			fakeServiceConfig: fmt.Sprintf(`{
				"name":"%s",
				"producer_project_id":"%s",
				"control" : {
					"environment": "servicecontrol.googleapis.com"
				},
                                "logging": {
                                   "producerDestinations": [{
                                       "logs": [
				          "endpoints_log"
				       ],
				       "monitoredResource": "api"
				   }]
				},
				"logs": [
				    {
				       "name": "endpoints_log"
				    }
				],
				"apis":[
					{
						"name":"%s",
						"version":"v1",
						"syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
							"fileName": "bookstore.proto"
						},
						"methods":[
							{
								"name": "ListShelves"
							},
							{
								"name": "CreateShelf"
							}
						]
					}
				],
				"http": {
					"rules": [
						{
							"selector": "endpoints.examples.bookstore.Bookstore.ListShelves",
							"get": "/v1/shelves"
						},
						{
							"selector": "endpoints.examples.bookstore.Bookstore.CreateShelf",
							"post": "/v1/shelves",
							"body": "shelf"
						}
					]
				}
			}`, testProjectName, testProjectID, testEndpointName),
			wantedListeners: fmt.Sprintf(`{
				"address":{
					"socketAddress":{
						"address":"0.0.0.0",
						"portValue":8080
					}
				},
				"filterChains":[
					{
						"filters":[
							{
								"config":{
									"http_filters":[
										{
											"config":{
												"gcp_attributes":{
													"platform": "GCE"
												},
												"rules":[
													{
														"pattern": {
															"http_method":"POST",
															"uri_template":"/endpoints.examples.bookstore.Bookstore/CreateShelf"
														},
														"requires":{
															"operation_name":"endpoints.examples.bookstore.Bookstore.CreateShelf",
															"service_name":"%s"
														}
													},
													{
														"pattern": {
															"http_method":"POST",
															"uri_template":"/v1/shelves"
														},
														"requires":{
															"operation_name":"endpoints.examples.bookstore.Bookstore.CreateShelf",
															"service_name":"%s"
														}
													},
													{
														"pattern": {
															"http_method":"POST",
															"uri_template":"/endpoints.examples.bookstore.Bookstore/ListShelves"
														},
														"requires":{
															"operation_name":"endpoints.examples.bookstore.Bookstore.ListShelves",
															"service_name":"%s"
														}
													},
													{
														"pattern": {
															"http_method":"GET",
															"uri_template":"/v1/shelves"
														},
														"requires":{
															"operation_name":"endpoints.examples.bookstore.Bookstore.ListShelves",
															"service_name":"%s"
														}
													}
												],
												"services":[
													{
														"service_control_uri":{
															"cluster":"service-control-cluster",
															"timeout":"5s",
															"uri":"https://servicecontrol.googleapis.com/v1/services/"
														},
														"service_name":"%s",
														"service_config_id":"%s",
														"producer_project_id":"%s",
														"token_cluster": "ads_cluster",
														"service_config":{
														   "@type":"type.googleapis.com/google.api.Service",
														   "logging":{"producer_destinations":[{"logs":["endpoints_log"],"monitored_resource":"api"}]},
														   "logs":[{"name":"endpoints_log"}]
														 }
													}
												]
											},
											"name":"envoy.filters.http.service_control"
										},
										{
											"config":{
											},
											"name":"envoy.grpc_web"
										},
										{
											"config":{
											},
											"name":"envoy.router"
										}
									],
									"route_config":{
										"name":"local_route",
										"virtual_hosts":[
											{
												"domains":[
													"*"
												],
												"name":"backend",
												"routes":[
													{
														"match":{
															"prefix":"/"
														},
														"route":{
															"cluster":"endpoints.examples.bookstore.Bookstore"
														}
													}
												]
											}
										]
									},
									"stat_prefix":"ingress_http"
								},
								"name":"envoy.http_connection_manager"
							}
						]
					}
				]
			}`, testProjectName, testProjectName, testProjectName, testProjectName, testProjectName, testConfigID, testProjectID),
		},
		{
			desc:            "Success for HTTP1 backend, with Jwt filter, with audiences",
			backendProtocol: "http1",
			fakeServiceConfig: fmt.Sprintf(`{
                "apis":[
                    {
                        "name":"%s"
                    }
                ],
                "http": {
                    "rules": [
                        {
                            "selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo_Auth_Jwt",
                            "get": "/auth/info/googlejwt"
                        },
                        {
                            "selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo",
                            "post": "/echo",
                            "body": "message"
                        }
                    ]
                },
                "authentication": {
                    "providers": [
                        {
                            "id": "firebase",
                            "issuer": "https://test_issuer.google.com/",
                            "jwks_uri": "$JWKSURI",
                            "audiences": "test_audience1, test_audience2 "
                        }
                    ],
                    "rules": [
                        {
                            "selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo"
                        },
                        {
                            "selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo_Auth_Jwt",
                            "requirements": [
                                {
                                    "provider_id": "firebase",
                                    "audiences": "test_audience1"
                                }
                            ]
                        }
                    ]
                }
            }`, testEndpointName),
			wantedListeners: fmt.Sprintf(`{
                "filters":[
                    {
                        "config":{
                            "http_filters":[
                                {
                                    "config": {
                                        "providers": {
                                            "firebase": {
                                                "audiences":["test_audience1", "test_audience2"],
                                                "issuer":"https://test_issuer.google.com/",
                                                "local_jwks": {
                                                    "inline_string": "%s"
                                                }
                                            }
                                        },
                                        "rules": [
                                            {
                                                "match":{
                                                    "headers":[
                                                        {
                                                            "exact_match":"GET",
                                                            "name":":method"
                                                        }
                                                    ],
                                                    "path":"/auth/info/googlejwt"
                                                },
                                                "requires": {
                                                    "provider_and_audiences": {
                                                    "audiences": ["test_audience1"],
                                                        "provider_name":"firebase"
                                                    }
                                                }
                                            }
                                        ]
                                    },
                                    "name":"envoy.filters.http.jwt_authn"
                                },
                                {
                                    "config":{
                                    },
                                    "name":"envoy.router"
                                 }
                            ],
                            "route_config":{
                                "name":"local_route",
                                "virtual_hosts":[
                                    {
                                        "domains":[
                                            "*"
                                        ],
                                        "name":"backend",
                                            "routes":[
                                                {
                                                    "match":{
                                                        "prefix":"/"
                                                    },
                                                    "route":{
                                                        "cluster": "%s"
                                                    }
                                                }
                                            ]
                                        }
                                    ]
                                },
                            "stat_prefix":"ingress_http"
                         },
                        "name":"envoy.http_connection_manager"
                    }
                ]
            }`, fakeJwks, testEndpointName),
		},
		{
			desc:            "Success for backend that allow CORS",
			backendProtocol: "http1",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
		"producer_project_id":"%s",
		"control" : {
			"environment": "servicecontrol.googleapis.com"
		},
		"apis":[
                   {
                        "name":"%s",
                        "methods":[
			{
				"name": "Simplegetcors"
			}
			]
                    }
                ],
                "http": {
                    "rules": [
                        {
                            "selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Simplegetcors",
                            "get": "/simplegetcors"
                        }
                    ]
                },
                "endpoints": [
		{
			"name": "%s",
			"allow_cors": true
		}
                ]
            }`, testProjectName, testProjectID, testEndpointName, testProjectName),
			wantedListeners: fmt.Sprintf(`{
			  "filters": [
			    {
			      "config": {
			        "http_filters": [
			          {
			            "config": {
						  "gcp_attributes":{
						 	 "platform": "GCE"
						  },
			              "rules": [
			                {
			                  "pattern": {
			                    "http_method": "GET",
			                    "uri_template": "/simplegetcors"
			                  },
			                  "requires": {
			                    "operation_name": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Simplegetcors",
			                    "service_name": "%s"
			                  }
			                },
			                {
			                  "pattern": {
			                    "http_method": "OPTIONS",
			                    "uri_template": "/simplegetcors"
			                  },
			                  "requires": {
			                    "api_key": {
			                      "allow_without_api_key": true
			                    },
			                    "operation_name": "CORS.0",
			                    "service_name": "%s"
			                  }
			                }
			              ],
			              "services": [
			                {
			                  "producer_project_id": "project123",
			                  "service_config_id": "2017-05-01r0",
			                  "service_control_uri": {
			                    "cluster": "service-control-cluster",
			                    "timeout": "5s",
			                    "uri": "https://servicecontrol.googleapis.com/v1/services/"
			                  },
			                  "service_name": "%s",
			                  "token_cluster": "ads_cluster",
					  "service_config":{"@type":"type.googleapis.com/google.api.Service"}
			                }
			              ]
			            },
			            "name": "envoy.filters.http.service_control"
			          },
			          {
			            "config": {},
			            "name": "envoy.router"
			          }
			        ],
			        "route_config": {
			          "name": "local_route",
			          "virtual_hosts": [
			            {
			              "domains": [
			                "*"
			              ],
			              "name": "backend",
			              "routes": [
			                {
			                  "match": {
			                    "prefix": "/"
			                  },
			                  "route": {
			                    "cluster": "%s"
			                  }
			                }
			              ]
			            }
			          ]
			        },
			        "stat_prefix": "ingress_http"
			      },
			      "name": "envoy.http_connection_manager"
			    }
			  ]
			}`, testProjectName, testProjectName, testProjectName, testEndpointName),
		},
	}

	for i, tc := range testData {
		// Overrides fakeConfig for the test case.
		fakeConfig = tc.fakeServiceConfig
		flag.Set("service", testProjectName)
		flag.Set("version", testConfigID)
		flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
		flag.Set("backend_protocol", tc.backendProtocol)

		runTest(t, func(env *testEnv) {
			ctx := context.Background()
			// First request, VersionId should be empty.
			req := v2.DiscoveryRequest{
				Node: &core.Node{
					Id: *flags.Node,
				},
				TypeUrl: cache.ListenerType,
			}
			resp, err := env.configManager.cache.Fetch(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			marshaler := &jsonpb.Marshaler{
				AnyResolver: ut.Resolver,
			}
			gotListeners, err := marshaler.MarshalToString(resp.Resources[0])
			if err != nil {
				t.Fatal(err)
			}

			if resp.Version != testConfigID {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got version: %v, want: %v", i, tc.desc, resp.Version, testConfigID)
			}
			if !reflect.DeepEqual(resp.Request, req) {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got request: %v, want: %v", i, tc.desc, resp.Request, req)
			}

			// Normalize both wantedListeners and gotListeners.
			gotListeners = normalizeJson(gotListeners)
			if want := normalizeJson(tc.wantedListeners); gotListeners != want && !strings.Contains(gotListeners, want) {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got unexpected Listeners", i, tc.desc)
				t.Errorf("Actual: %s", gotListeners)
				t.Errorf("Expected: %s", want)
			}
		})
	}
}

func TestDynamicBackendRouting(t *testing.T) {
	testData := []struct {
		desc              string
		fakeServiceConfig string
		backendProtocol   string
		wantedClusters    []string
		wantedListener    string
	}{
		{
			desc:              "Success for http1 with dynamic routing",
			fakeServiceConfig: marshalServiceConfigToString(testdata.FakeConfigForDynamicRouting, t),
			backendProtocol:   "http1",
			wantedClusters:    testdata.FakeWantedClustersForDynamicRouting,
			wantedListener:    testdata.FakeWantedListenerForDynamicRouting,
		},
	}

	marshaler := &jsonpb.Marshaler{}
	for i, tc := range testData {
		// Overrides fakeConfig for the test case.
		fakeConfig = tc.fakeServiceConfig
		flag.Set("service", testProjectName)
		flag.Set("version", testConfigID)
		flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
		flag.Set("backend_protocol", tc.backendProtocol)
		flag.Set("enable_backend_routing", "true")

		runTest(t, func(env *testEnv) {
			ctx := context.Background()
			// First request, VersionId should be empty.
			reqForClusters := v2.DiscoveryRequest{
				Node: &core.Node{
					Id: *flags.Node,
				},
				TypeUrl: cache.ClusterType,
			}

			respForClusters, err := env.configManager.cache.Fetch(ctx, reqForClusters)
			if err != nil {
				t.Fatal(err)
			}

			if respForClusters.Version != testConfigID {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got version: %v, want: %v", i, tc.desc, respForClusters.Version, testConfigID)
			}
			if !reflect.DeepEqual(respForClusters.Request, reqForClusters) {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got request: %v, want: %v", i, tc.desc, respForClusters.Request, reqForClusters)
			}

			sortedClusters := sortResources(respForClusters)
			for idx, want := range tc.wantedClusters {
				gotCluster, err := marshaler.MarshalToString(sortedClusters[idx])
				if err != nil {
					t.Fatal(err)
				}
				gotCluster = normalizeJson(gotCluster)
				if want = normalizeJson(want); gotCluster != want {
					t.Errorf("Test Desc(%d): %s, idx %d snapshot cache fetch got Cluster: %s, want: %s", i, tc.desc, idx, gotCluster, want)
				}
			}

			reqForListener := v2.DiscoveryRequest{
				Node: &core.Node{
					Id: *flags.Node,
				},
				TypeUrl: cache.ListenerType,
			}

			respForListener, err := env.configManager.cache.Fetch(ctx, reqForListener)
			if err != nil {
				t.Fatal(err)
			}
			if respForListener.Version != testConfigID {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got version: %v, want: %v", i, tc.desc, respForListener.Version, testConfigID)
			}
			if !reflect.DeepEqual(respForListener.Request, reqForListener) {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got request: %v, want: %v", i, tc.desc, respForListener.Request, reqForListener)
			}

			gotListener, err := marshaler.MarshalToString(respForListener.Resources[0])
			if err != nil {
				t.Fatal(err)
			}
			gotListener = normalizeJson(gotListener)
			if wantListener := normalizeJson(tc.wantedListener); gotListener != wantListener {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got Listener: %s, want: %s", i, tc.desc, gotListener, wantListener)
			}
		})
	}
}

func TestPathMatcherFilter(t *testing.T) {
	testData := []struct {
		desc                  string
		fakeServiceConfig     string
		backendProtocol       string
		wantPathMatcherFilter string
	}{
		{
			desc: "Path Matcher filter - gRPC backend",
			fakeServiceConfig: fmt.Sprintf(`{
				"name":"%s",
				"apis":[
					{
						"name":"%s",
						"version":"v1",
						"syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
							"fileName": "bookstore.proto"
						},
						"methods":[
							{
								"name": "ListShelves"
							},
							{
								"name": "CreateShelf"
							}
						]
					}
				],
				"sourceInfo":{
					"sourceFiles":[
						{
							"@type":"type.googleapis.com/google.api.servicemanagement.v1.ConfigFile",
							"filePath":"api_descriptor.pb",
							"fileContents":"%s",
							"fileType":"FILE_DESCRIPTOR_SET_PROTO"
						}
					]
				}
			}`, testProjectName, testEndpointName, fakeProtoDescriptor),
			backendProtocol: "gRPC",
			wantPathMatcherFilter: `
              {
                "config": {
                  "rules": [
                    {
                      "operation": "endpoints.examples.bookstore.Bookstore.ListShelves",
                      "pattern": {
                        "http_method": "POST",
                        "uri_template": "/endpoints.examples.bookstore.Bookstore/ListShelves"
                      }
                    },
                    {
                      "operation": "endpoints.examples.bookstore.Bookstore.CreateShelf",
                      "pattern": {
                        "http_method": "POST",
                        "uri_template": "/endpoints.examples.bookstore.Bookstore/CreateShelf"
                      }
                    }
                  ]
                },
                "name": "envoy.filters.http.path_matcher"
              }`,
		},
		{
			desc: "Path Matcher filter - HTTP backend",
			fakeServiceConfig: fmt.Sprintf(`{
				"name":"%s",
				"apis":[
					{
						"name":"%s"
					}
				],
				"http": {
 					"rules": [
						{
							"selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo_Auth_Jwt",
							"get": "/auth/info/googlejwt"
						},
						{
							"selector": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo",
							"post": "/echo",
							"body": "message"
						}
					]
				}
			}`, testProjectName, testEndpointName),
			backendProtocol: "http1",
			wantPathMatcherFilter: `
              {
                "config": {
                  "rules": [
                    {
                      "operation": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo_Auth_Jwt",
                      "pattern": {
                        "http_method": "GET",
                        "uri_template": "/auth/info/googlejwt"
                      }
                    },
                    {
                      "operation": "1.echo_api_endpoints_cloudesf_testing_cloud_goog.Echo",
                      "pattern": {
                        "http_method": "POST",
                        "uri_template": "/echo"
                      }
                    }
                  ]
                },
                "name": "envoy.filters.http.path_matcher"
              }`,
		},
		{
			desc: "Path Matcher filter - HTTP backend with path parameters",
			fakeServiceConfig: `{
				"name":"foo.endpoints.bar.cloud.goog",
				"apis":[
					{
						"name":"endpoints.test.foo.Bar"
					}
				],
				"backend": {
				  "rules": [
					{
					  "selector": "1.cloudesf_testing_cloud_goog.Foo",
					  "pathTranslation": "CONSTANT_ADDRESS",
					  "jwtAudience": "mybackend.com"
					},
					{
					  "selector": "1.cloudesf_testing_cloud_goog.Bar",
					  "pathTranslation": "APPEND_PATH_TO_ADDRESS",
					  "jwtAudience": "mybackend.com"
					}
				  ]
				},
                "http": {
                    "rules": [
                        {
                           "selector": "1.cloudesf_testing_cloud_goog.Foo",
                           "get": "foo/{id}"
                        },
                        {
                            "selector": "1.cloudesf_testing_cloud_goog.Bar",
                            "get": "foo"
                        }
                    ]
                }
			}`,
			backendProtocol: "http1",
			wantPathMatcherFilter: `
              {
                "config": {
                  "rules": [
                    {
                      "extract_path_parameters": true,
                      "operation": "1.cloudesf_testing_cloud_goog.Foo",
                      "pattern": {
                        "http_method": "GET",
                        "uri_template": "foo/{id}"
                      }
                    },
                    {
                      "operation": "1.cloudesf_testing_cloud_goog.Bar",
                      "pattern": {
                        "http_method": "GET",
                        "uri_template": "foo"
                      }
                    }
                  ]
                },
                "name": "envoy.filters.http.path_matcher"
              }`,
		},
	}

	for i, tc := range testData {
		// Overriding fakeConfig.
		fakeConfig = tc.fakeServiceConfig
		flag.Set("service", testProjectName)
		flag.Set("version", testConfigID)
		flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
		flag.Set("backend_protocol", tc.backendProtocol)
		// TODO(kyuc): modify this when filters other than Backend Auth filter
		// use Path Matcher filter.
		flag.Set("enable_backend_routing", "true")

		runTest(t, func(env *testEnv) {
			ctx := context.Background()
			// First request, VersionId should be empty.
			req := v2.DiscoveryRequest{
				Node: &core.Node{
					Id: *flags.Node,
				},
				TypeUrl: cache.ListenerType,
			}
			resp, err := env.configManager.cache.Fetch(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			marshaler := &jsonpb.Marshaler{
				AnyResolver: ut.Resolver,
			}
			gotListeners, err := marshaler.MarshalToString(resp.Resources[0])
			if err != nil {
				t.Fatal(err)
			}

			// Normalize both path matcher filter and gotListeners.
			gotListeners = normalizeJson(gotListeners)
			want := normalizeJson(tc.wantPathMatcherFilter)
			if !strings.Contains(gotListeners, want) {
				t.Errorf("Test Desc(%d): %s, expected Path Matcher filter was not in the listeners", i, tc.desc)
				t.Errorf("Actual: %s", gotListeners)
				t.Errorf("Expected: %s", want)
			}
		})
	}
}

func TestBackendAuthFilter(t *testing.T) {
	fakeServiceConfig := fmt.Sprintf(`{
										"name":"%s",
										"apis":[
											{
												"name":"%s"
											}
										],
										"backend": {
										  "rules": [
											{
											  "selector": "foo",
											  "jwtAudience": "foo.com"
											},
											{
											  "selector": "bar",
											  "jwtAudience": "bar.com"
											}
										  ]
										}
									}`, testProjectName, testEndpointName)

	wantBackendAuthFilter := `{
								"config": {
								  "rules": [
									{
									  "jwt_audience": "foo.com",
									  "operation": "foo",
									  "token_cluster": "ads_cluster"
									},
									{
									  "jwt_audience": "bar.com",
									  "operation": "bar",
									  "token_cluster": "ads_cluster"
									}
								  ]
								},
								"name": "envoy.filters.http.backend_auth"
							  }`

	// Overriding fakeConfig.
	fakeConfig = fakeServiceConfig
	flag.Set("service", testProjectName)
	flag.Set("version", testConfigID)
	flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
	flag.Set("backend_protocol", "http1")
	flag.Set("enable_backend_routing", "true")

	runTest(t, func(env *testEnv) {
		ctx := context.Background()
		// First request, VersionId should be empty.
		req := v2.DiscoveryRequest{
			Node: &core.Node{
				Id: *flags.Node,
			},
			TypeUrl: cache.ListenerType,
		}
		resp, err := env.configManager.cache.Fetch(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		marshaler := &jsonpb.Marshaler{
			AnyResolver: ut.Resolver,
		}
		gotListeners, err := marshaler.MarshalToString(resp.Resources[0])
		if err != nil {
			t.Fatal(err)
		}

		// Normalize both path matcher filter and gotListeners.
		gotListeners = normalizeJson(gotListeners)
		want := normalizeJson(wantBackendAuthFilter)
		if !strings.Contains(gotListeners, want) {
			t.Errorf("Expected Backend Auth filter was not in the listeners")
			t.Errorf("Actual: %s", gotListeners)
			t.Errorf("Expected: %s", want)
		}
	})
}

func TestFetchClusters(t *testing.T) {
	testData := []struct {
		desc              string
		fakeServiceConfig string
		wantedClusters    []string
		backendProtocol   string
	}{
		{
			desc: "Success for gRPC backend",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
		"control" : {
			"environment": "servicecontrol.googleapis.com"
		},
                "apis":[
                    {
                        "name":"%s",
                        "version":"v1",
                        "syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
							"fileName": "bookstore.proto"
						}
					}
                ]
		    }`, testProjectName, testEndpointName),
			backendProtocol: "grpc",
			wantedClusters: []string{
				fmt.Sprintf(`{
	    	    "hosts": [
	    	        {
	    	      	    "socketAddress": {
	    	      	  	    "address": "%s",
	    	      	  	    "portValue": %d
	    	      	    }
	    	        }
	    	    ],
	    	    "name": "%s",
	    	    "connectTimeout": "%ds",
                "type":"STRICT_DNS",
                "http2ProtocolOptions": {}
	        }`, *flags.ClusterAddress, *flags.ClusterPort, testEndpointName, *flags.ClusterConnectTimeout/1e9),
				`{"name": "service-control-cluster",
		"connectTimeout": "5s",
		"type": "LOGICAL_DNS",
		"dnsLookupFamily": "V4_ONLY",
	    	"hosts": [ {
    	      	    "socketAddress": {
    	      	  	  "address": "servicecontrol.googleapis.com",
	    	      	  "portValue": 443
    	      	    }
    	        } ],
		"tlsContext": { "sni": "servicecontrol.googleapis.com" }
		}`,
			},
		},
		{
			desc: "Success for HTTP1 backend",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
		"control" : {
			"environment": "http://127.0.0.1:8000"
		},
                "apis":[
                    {
                        "name":"%s"
                    }
                ]
            }`, testProjectName, testEndpointName),
			backendProtocol: "http1",
			wantedClusters: []string{
				fmt.Sprintf(`{
                "hosts": [
                    {
                        "socketAddress": {
                            "address": "%s",
                            "portValue": %d
                        }
                    }
                ],
                "name": "%s",
                "connectTimeout": "%ds",
                "type":"STRICT_DNS"
           }`, *flags.ClusterAddress, *flags.ClusterPort, testEndpointName, *flags.ClusterConnectTimeout/1e9),
				`{"name": "service-control-cluster",
		"connectTimeout": "5s",
		"type": "LOGICAL_DNS",
		"dnsLookupFamily": "V4_ONLY",
	    	"hosts": [ {
    	      	    "socketAddress": {
    	      	  	  "address": "127.0.0.1",
	    	      	  "portValue": 8000
    	      	    }
    	        } ]
		}`,
			},
		},
	}

	for i, tc := range testData {
		// Overrides fakeConfig for the test case.
		fakeConfig = tc.fakeServiceConfig
		flag.Set("service", testProjectName)
		flag.Set("version", testConfigID)
		flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
		flag.Set("backend_protocol", tc.backendProtocol)

		runTest(t, func(env *testEnv) {
			ctx := context.Background()
			// First request, VersionId should be empty.
			req := v2.DiscoveryRequest{
				Node: &core.Node{
					Id: *flags.Node,
				},
				TypeUrl: cache.ClusterType,
			}

			resp, err := env.configManager.cache.Fetch(ctx, req)
			if err != nil {
				t.Fatal(err)
			}

			if resp.Version != testConfigID {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got version: %v, want: %v", i, tc.desc, resp.Version, testConfigID)
			}
			if !reflect.DeepEqual(resp.Request, req) {
				t.Errorf("Test Desc(%d): %s, snapshot cache fetch got request: %v, want: %v", i, tc.desc, resp.Request, req)
			}

			// configManager.cache may change the order
			// sort them before comparing results.
			sorted_sources := resp.Resources
			sort.Slice(sorted_sources, func(i, j int) bool {
				return cache.GetResourceName(sorted_sources[i]) < cache.GetResourceName(sorted_sources[j])
			})

			for idx, want := range tc.wantedClusters {
				marshaler := &jsonpb.Marshaler{}
				gotClusters, err := marshaler.MarshalToString(sorted_sources[idx])
				if err != nil {
					t.Fatal(err)
				}

				gotClusters = normalizeJson(gotClusters)
				if want = normalizeJson(want); gotClusters != want {
					t.Errorf("Test Desc(%d): %s, idx %d snapshot cache fetch got Clusters: %s, want: %s", i, tc.desc, idx, gotClusters, want)
				}
			}
		})
	}
}

func TestMakeRouteConfig(t *testing.T) {
	testData := []struct {
		desc string
		// Test parameters, in the order of "cors_preset", "cors_allow_origin"
		// "cors_allow_origin_regex", "cors_allow_methods", "cors_allow_headers"
		// "cors_expose_headers"
		params           []string
		allowCredentials bool
		wantedError      string
		wantRoute        *route.CorsPolicy
	}{
		{
			desc:      "No Cors",
			wantRoute: nil,
		},
		{
			desc:        "Incorrect configured basic Cors",
			params:      []string{"basic", "", `^https?://.+\\.example\\.com$`, "", "", ""},
			wantedError: "cors_allow_origin cannot be empty when cors_preset=basic",
		},
		{
			desc:        "Incorrect configured  Cors",
			params:      []string{"", "", "", "GET", "", ""},
			wantedError: "cors_preset must be set in order to enable CORS support",
		},
		{
			desc:        "Incorrect configured regex Cors",
			params:      []string{"cors_with_regexs", "", `^https?://.+\\.example\\.com$`, "", "", ""},
			wantedError: `cors_preset must be either "basic" or "cors_with_regex"`,
		},
		{
			desc:   "Correct configured basic Cors, with allow methods",
			params: []string{"basic", "http://example.com", "", "GET,POST,PUT,OPTIONS", "", ""},
			wantRoute: &route.CorsPolicy{
				AllowOrigin:      []string{"http://example.com"},
				AllowMethods:     "GET,POST,PUT,OPTIONS",
				AllowCredentials: &types.BoolValue{Value: false},
			},
		},
		{
			desc:   "Correct configured regex Cors, with allow headers",
			params: []string{"cors_with_regex", "", `^https?://.+\\.example\\.com$`, "", "Origin,Content-Type,Accept", ""},
			wantRoute: &route.CorsPolicy{
				AllowOriginRegex: []string{`^https?://.+\\.example\\.com$`},
				AllowHeaders:     "Origin,Content-Type,Accept",
				AllowCredentials: &types.BoolValue{Value: false},
			},
		},
		{
			desc:             "Correct configured regex Cors, with expose headers",
			params:           []string{"cors_with_regex", "", `^https?://.+\\.example\\.com$`, "", "", "Content-Length"},
			allowCredentials: true,
			wantRoute: &route.CorsPolicy{
				AllowOriginRegex: []string{`^https?://.+\\.example\\.com$`},
				ExposeHeaders:    "Content-Length",
				AllowCredentials: &types.BoolValue{Value: true},
			},
		},
	}

	for _, tc := range testData {
		// Initial flags
		if tc.params != nil {
			flag.Set("cors_preset", tc.params[0])
			flag.Set("cors_allow_origin", tc.params[1])
			flag.Set("cors_allow_origin_regex", tc.params[2])
			flag.Set("cors_allow_methods", tc.params[3])
			flag.Set("cors_allow_headers", tc.params[4])
			flag.Set("cors_expose_headers", tc.params[5])
		}
		flag.Set("cors_allow_credentials", strconv.FormatBool(tc.allowCredentials))

		gotRoute, err := makeRouteConfig(&api.Api{Name: "test-api"})
		if tc.wantedError != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantedError) {
				t.Errorf("Test (%s): expected err: %v, got: %v", tc.desc, tc.wantedError, err)
			}
			continue
		}

		gotHost := gotRoute.GetVirtualHosts()
		if len(gotHost) != 1 {
			t.Errorf("Test (%s): got expected number of virtual host", tc.desc)
		}
		gotCors := gotHost[0].GetCors()
		if !reflect.DeepEqual(gotCors, tc.wantRoute) {
			t.Errorf("Test (%s): makeRouteConfig failed, got Cors: %s, want: %s", tc.desc, gotCors, tc.wantRoute)
		}
	}
}

func TestServiceConfigAutoUpdate(t *testing.T) {
	var oldConfigID, oldRolloutID, newConfigID, newRolloutID string
	oldConfigID = "2018-12-05r0"
	oldRolloutID = oldConfigID
	newConfigID = "2018-12-05r1"
	newRolloutID = newConfigID
	testCase := struct {
		desc                  string
		fakeOldServiceRollout string
		fakeNewServiceRollout string
		fakeOldServiceConfig  string
		fakeNewServiceConfig  string
		backendProtocol       string
	}{
		desc: "Success for service config auto update",
		fakeOldServiceRollout: fmt.Sprintf(`{
			"rollouts": [
			    {
			      "rolloutId": "%s",
			      "createTime": "2018-12-05T19:07:18.438Z",
			      "createdBy": "mocktest@google.com",
			      "status": "SUCCESS",
			      "trafficPercentStrategy": {
			        "percentages": {
			          "%s": 100
			        }
			      },
			      "serviceName": "%s"
			    }
			  ]
			}`, oldRolloutID, oldConfigID, testProjectName),
		fakeNewServiceRollout: fmt.Sprintf(`{
			"rollouts": [
			    {
			      "rolloutId": "%s",
			      "createTime": "2018-12-05T19:07:18.438Z",
			      "createdBy": "mocktest@google.com",
			      "status": "SUCCESS",
			      "trafficPercentStrategy": {
			        "percentages": {
			          "%s": 40,
			          "%s": 60
			        }
			      },
			      "serviceName": "%s"
			    },
			    {
			      "rolloutId": "%s",
			      "createTime": "2018-12-05T19:07:18.438Z",
			      "createdBy": "mocktest@google.com",
			      "status": "SUCCESS",
			      "trafficPercentStrategy": {
			        "percentages": {
			          "%s": 100
			        }
			      },
			      "serviceName": "%s"
			    }
			  ]
			}`, newRolloutID, oldConfigID, newConfigID, testProjectName,
			oldRolloutID, oldConfigID, testProjectName),
		fakeOldServiceConfig: fmt.Sprintf(`{
				"name": "%s",
				"title": "Endpoints Example",
				"documentation": {
				"summary": "A simple Google Cloud Endpoints API example."
				},
				"apis":[
					{
						"name":"%s"
					}
				],
				"id": "%s"
			}`, testProjectName, testEndpointName, oldConfigID),
		fakeNewServiceConfig: fmt.Sprintf(`{
				"name": "%s",
				"title": "Endpoints Example",
				"documentation": {
				"summary": "A simple Google Cloud Endpoints API example."
				},
				"apis":[
					{
						"name":"%s"
					}
				],
				"id": "%s"
			}`, testProjectName, testEndpointName, newConfigID),
		backendProtocol: "grpc",
	}

	// Overrides fakeConfig with fakeOldServiceConfig for the test case.
	fakeConfig = testCase.fakeOldServiceConfig
	fakeRollout = testCase.fakeOldServiceRollout
	checkNewRolloutInterval = 1 * time.Second
	flag.Set("service", testProjectName)
	flag.Set("version", testConfigID)
	flag.Set("rollout_strategy", ut.ManagedRolloutStrategy)
	flag.Set("backend_protocol", testCase.backendProtocol)

	runTest(t, func(env *testEnv) {
		var resp *cache.Response
		var err error
		ctx := context.Background()
		req := v2.DiscoveryRequest{
			Node: &core.Node{
				Id: *flags.Node,
			},
			TypeUrl: cache.ListenerType,
		}
		resp, err = env.configManager.cache.Fetch(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		if resp.Version != oldConfigID {
			t.Errorf("Test Desc: %s, snapshot cache fetch got version: %v, want: %v", testCase.desc, resp.Version, oldConfigID)
		}
		if env.configManager.curRolloutID != oldRolloutID {
			t.Errorf("Test Desc: %s, config manager rollout id: %v, want: %v", testCase.desc, env.configManager.curRolloutID, oldRolloutID)
		}
		if !reflect.DeepEqual(resp.Request, req) {
			t.Errorf("Test Desc: %s, snapshot cache fetch got request: %v, want: %v", testCase.desc, resp.Request, req)
		}

		fakeConfig = testCase.fakeNewServiceConfig
		fakeRollout = testCase.fakeNewServiceRollout
		time.Sleep(time.Duration(checkNewRolloutInterval + time.Second))

		resp, err = env.configManager.cache.Fetch(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		if resp.Version != newConfigID {
			t.Errorf("Test Desc: %s, snapshot cache fetch got version: %v, want: %v", testCase.desc, resp.Version, newConfigID)
		}
		if env.configManager.curRolloutID != newRolloutID {
			t.Errorf("Test Desc: %s, config manager rollout id: %v, want: %v", testCase.desc, env.configManager.curRolloutID, newRolloutID)
		}
		if !reflect.DeepEqual(resp.Request, req) {
			t.Errorf("Test Desc: %s, snapshot cache fetch got request: %v, want: %v", testCase.desc, resp.Request, req)
		}
	})
}

func TestGetEndpointAllowCorsFlag(t *testing.T) {
	testData := []struct {
		desc                string
		fakeServiceConfig   string
		wantedAllowCorsFlag bool
	}{
		{
			desc: "Return true for endpoint name matching service name",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
                "apis":[
                    {
                        "name":"%s",
                        "version":"v1",
                        "syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
														"fileName": "bookstore.proto"
												}
										}
                ],
								"endpoints": [
										{
													"name": "%s",
													"allow_cors": true
										}
                ]
		    }`, testProjectName, testEndpointName, testProjectName),
			wantedAllowCorsFlag: true,
		},
		{
			desc: "Return false for not setting allow_cors",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
                "apis":[
                    {
                        "name":"%s",
                        "version":"v1",
                        "syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
														"fileName": "bookstore.proto"
												}
										}
                ],
								"endpoints": [
										{
													"name": "%s"
										}
                ]
		    }`, testProjectName, testEndpointName, testProjectName),
			wantedAllowCorsFlag: false,
		},
		{
			desc: "Return false for endpoint name not matching service name",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
                "apis":[
                    {
                        "name":"%s",
                        "version":"v1",
                        "syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
														"fileName": "bookstore.proto"
												}
										}
                ],
								"endpoints": [
										{
													"name": "%s",
													"allow_cors": true
										}
                ]
		    }`, testProjectName, testEndpointName, "echo.endpoints.project123.cloud.goog"),
			wantedAllowCorsFlag: false,
		},
		{
			desc: "Return false for empty endpoint field",
			fakeServiceConfig: fmt.Sprintf(`{
                "name":"%s",
                "apis":[
                    {
                        "name":"%s",
                        "version":"v1",
                        "syntax":"SYNTAX_PROTO3",
                        "sourceContext": {
														"fileName": "bookstore.proto"
												}
										}
                ]
		    }`, testProjectName, testEndpointName),
			wantedAllowCorsFlag: false,
		},
	}

	for i, tc := range testData {
		// Overrides fakeConfig for the test case.
		fakeConfig = tc.fakeServiceConfig
		flag.Set("service", testProjectName)
		flag.Set("version", testConfigID)
		flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
		flag.Set("backend_protocol", "http1")

		runTest(t, func(env *testEnv) {
			allowCorsFlag := env.configManager.getEndpointAllowCorsFlag()
			if allowCorsFlag != tc.wantedAllowCorsFlag {
				t.Errorf("Test Desc(%d): %s, allow CORS flag got: %v, want: %v", i, tc.desc, allowCorsFlag, tc.wantedAllowCorsFlag)
			}
		})
	}
}

func TestExtractBackendAddress(t *testing.T) {
	fakeServiceConfig := fmt.Sprintf(`{
				"name":"%s",
				"apis":[
					{
						"name":"%s",
						"version":"v1",
						"syntax":"SYNTAX_PROTO3",
						"sourceContext": {
							"fileName": "bookstore.proto"
						}
					}
				],
				"endpoints": [
					{
						"name": "%s"
					}
				]
			}`, testProjectName, testEndpointName, testProjectName)
	testData := []struct {
		desc           string
		url            string
		wantedHostname string
		wantedPort     uint32
		wantedErr      string
	}{
		{
			desc:           "successful for https url, ends without slash",
			url:            "https://abc.example.org",
			wantedHostname: "abc.example.org",
			wantedPort:     443,
			wantedErr:      "",
		},
		{
			desc:           "successful for https url, ends with slash",
			url:            "https://abcde.google.org/",
			wantedHostname: "abcde.google.org",
			wantedPort:     443,
			wantedErr:      "",
		},
		{
			desc:           "successful for https url, ends with path",
			url:            "https://abcde.youtube.com/api/",
			wantedHostname: "abcde.youtube.com",
			wantedPort:     443,
			wantedErr:      "",
		},
		{
			desc:           "successful for https url with custome port",
			url:            "https://abcde.youtube.com:8989/api/",
			wantedHostname: "abcde.youtube.com",
			wantedPort:     8989,
			wantedErr:      "",
		},
		{
			desc:           "fail for http url",
			url:            "http://abcde.youtube.com:8989/api/",
			wantedHostname: "",
			wantedPort:     0,
			wantedErr:      "dynamic routing only supports HTTPS",
		},
		{
			desc:           "fail for https url with IP address",
			url:            "https://192.168.0.1/api/",
			wantedHostname: "",
			wantedPort:     0,
			wantedErr:      "dynamic routing only supports domain name, got IP address: 192.168.0.1",
		},
	}

	fakeConfig = fakeServiceConfig
	for i, tc := range testData {
		// Overrides fakeConfig for the test case.
		flag.Set("service", testProjectName)
		flag.Set("version", testConfigID)
		flag.Set("rollout_strategy", ut.FixedRolloutStrategy)
		flag.Set("backend_protocol", "http1")

		runTest(t, func(env *testEnv) {
			hostname, port, err := env.configManager.extractBackendAddress(tc.url)
			if hostname != tc.wantedHostname {
				t.Errorf("Test Desc(%d): %s, extract backend address got: %v, want: %v", i, tc.desc, hostname, tc.wantedHostname)
			}
			if port != tc.wantedPort {
				t.Errorf("Test Desc(%d): %s, extract backend address got: %v, want: %v", i, tc.desc, port, tc.wantedPort)
			}
			if (err == nil && tc.wantedErr != "") || (err != nil && tc.wantedErr == "") {
				t.Errorf("Test Desc(%d): %s, extract backend address got: %v, want: %v", i, tc.desc, err, tc.wantedErr)
			}
		})
	}
}

// Test Environment setup.

type testEnv struct {
	configManager *ConfigManager
}

func runTest(t *testing.T, f func(*testEnv)) {
	mockConfig := initMockConfigServer(t)
	defer mockConfig.Close()
	fetchConfigURL = func(serviceName, configID string) string {
		return mockConfig.URL
	}

	mockRollout := initMockRolloutServer(t)
	defer mockRollout.Close()
	fetchRolloutsURL = func(serviceName string) string {
		return mockRollout.URL
	}

	mockMetadata := initMockMetadataServerFromPathResp(
		map[string]string{
			util.ServiceAccountTokenSuffix: fakeToken,
		})
	defer mockMetadata.Close()
	fetchMetadataURL = func(suffix string) string {
		return mockMetadata.URL + suffix
	}

	mockJwksIssuer := initMockJwksIssuer(t)
	defer mockJwksIssuer.Close()

	// Replace $JWKSURI here, since it depends on the mock server.
	fakeConfig = strings.Replace(fakeConfig, "$JWKSURI", mockJwksIssuer.URL, -1)
	manager, err := NewConfigManager()
	if err != nil {
		t.Fatal("fail to initialize ConfigManager: ", err)
	}
	env := &testEnv{
		configManager: manager,
	}
	f(env)
}

func initMockConfigServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(normalizeJson(fakeConfig)))
	}))
}

func initMockRolloutServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(normalizeJson(fakeRollout)))
	}))
}

func initMockJwksIssuer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fakeJwks))
	}))
}

func sortResources(response *cache.Response) []cache.Resource {
	// configManager.cache may change the order
	// sort them before comparing results.
	sortedResources := response.Resources
	sort.Slice(sortedResources, func(i, j int) bool {
		return cache.GetResourceName(sortedResources[i]) < cache.GetResourceName(sortedResources[j])
	})
	return sortedResources
}

func marshalServiceConfigToString(serviceConfig *conf.Service, t *testing.T) string {
	m := &jsonpb.Marshaler{}
	jsonStr, err := m.MarshalToString(serviceConfig)
	if err != nil {
		t.Fatal("fail to convert service config to string: ", err)
	}
	return jsonStr
}

type mock struct{}

func (mock) ID(*core.Node) string {
	return fakeNodeID
}

func normalizeJson(input string) string {
	var jsonObject map[string]interface{}
	json.Unmarshal([]byte(input), &jsonObject)
	outputString, _ := json.Marshal(jsonObject)
	return string(outputString)
}
